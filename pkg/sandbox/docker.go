package sandbox

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/config"
)

const defaultDockerImage = "ubuntu:24.04"

var dockerNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`) //nolint:gochecknoglobals

// DockerRuntime prepares one-shot docker run invocations for sandboxed commands.
// The docker CLI owns container lifecycle through --rm, so Cleanup is a no-op.
type DockerRuntime struct {
	cfg        *config.SandboxConfig
	workingDir string
	image      string
	publisher  EventPublisher
}

// NewDockerRuntime creates a Docker-backed runtime using one-shot containers.
func NewDockerRuntime(cfg *config.SandboxConfig, workingDir string) Runtime {
	return NewDockerRuntimeWithEventPublisher(cfg, workingDir, nil)
}

// NewDockerRuntimeWithEventPublisher creates a Docker runtime that emits sandbox events.
func NewDockerRuntimeWithEventPublisher(cfg *config.SandboxConfig, workingDir string, publisher EventPublisher) Runtime {
	image := defaultDockerImage
	if cfg != nil && cfg.DockerImage != "" {
		image = cfg.DockerImage
	}
	return &DockerRuntime{
		cfg:        cfg,
		workingDir: workingDir,
		image:      image,
		publisher:  publisher,
	}
}

// PrepareCommand builds a docker run command that executes req.Command inside a
// temporary container with the configured mounts and network policy.
func (r *DockerRuntime) PrepareCommand(ctx context.Context, req SandboxRequest) (*SandboxEnvironment, error) {
	workingDir := req.WorkingDir
	if workingDir == "" {
		workingDir = r.workingDir
	}
	network := req.Network
	if network == "" {
		network = networkPolicyFromConfig(r.cfg)
	}
	if network == NetworkInherited {
		network = NetworkAllowed
	}

	mounts := mountsFromConfig(r.cfg, workingDir)
	lifecycle := dockerLifecycleFromConfig(r.cfg, req.Metadata)
	containerName := dockerContainerName(lifecycle, workingDir, req.Metadata)
	args := []string{"run"}
	if lifecycle == "command" {
		args = append(args, "--rm")
	}
	if network == NetworkDenied {
		args = append(args, "--network", "none")
	}
	args = append(args, dockerResourceArgs(req.ResourceLimits)...)
	containerEnv := dockerContainerEnv(req.Env)
	for _, env := range containerEnv {
		args = append(args, "-e", env)
	}
	for _, m := range mounts {
		mode := "ro"
		if m.Mode == MountReadWrite {
			mode = "rw"
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", m.HostPath, m.Path, mode))
	}
	if workingDir != "" {
		args = append(args, "-w", workingDir)
	}
	args = append(args, r.image, req.Command)
	args = append(args, req.Args...)
	command := "docker"
	if lifecycle != "command" {
		runArgs := dockerPersistentRunArgs(containerName, r.image, network, containerEnv, mounts, workingDir, req.ResourceLimits)
		execArgs := dockerExecArgs(containerName, workingDir, req.Command, req.Args)
		args = []string{"-c", dockerPersistentScript(containerName, runArgs, execArgs)}
		command = "sh"
	}

	metadata := copyMetadata(req.Metadata)
	metadata["mode"] = "command"
	metadata["image"] = r.image
	metadata["lifecycle"] = lifecycle
	if containerName != "" {
		metadata["container_name"] = containerName
	}

	env := &SandboxEnvironment{
		Backend:        BackendDocker,
		BackendDetail:  "docker",
		Enabled:        true,
		Command:        command,
		Args:           args,
		WorkingDir:     workingDir,
		EnvNames:       extractEnvNames(containerEnv),
		Network:        network,
		Mounts:         mounts,
		ResourceLimits: req.ResourceLimits,
		Metadata:       metadata,
	}
	publisher := publisherForContext(ctx, r.publisher)
	PublishEvent(publisher, EventTypeSandboxDecisionCreated, env, "sandbox decision created", nil)
	PublishEvent(publisher, EventTypeSandboxEnvironmentCreated, env, "sandbox environment created", nil)
	return env, nil
}

// Cleanup is a no-op for one-shot docker run --rm containers.
func (r *DockerRuntime) Cleanup(ctx context.Context, env *SandboxEnvironment) error {
	PublishEvent(publisherForContext(ctx, r.publisher), EventTypeSandboxEnvironmentCleaned, env, "sandbox environment cleaned", nil)
	return nil
}

// dockerContainerEnv intentionally propagates only nano-managed variables into
// containers to avoid leaking host secrets such as cloud tokens or SSH agent data.
// Do not place secrets in NANO_* variables unless the sandboxed command is meant
// to receive them.
func dockerContainerEnv(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		if strings.HasPrefix(name, "NANO_") {
			out = append(out, entry)
		}
	}
	return out
}

func dockerLifecycleFromConfig(cfg *config.SandboxConfig, metadata map[string]interface{}) string {
	if value, ok := metadata["sandbox_lifecycle"].(string); ok && strings.TrimSpace(value) != "" {
		return normalizeDockerLifecycle(value)
	}
	if cfg != nil && cfg.DockerLifecycle != "" {
		return normalizeDockerLifecycle(cfg.DockerLifecycle)
	}
	return "command"
}

func normalizeDockerLifecycle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "task", "session":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "command"
	}
}

func dockerContainerName(lifecycle, workingDir string, metadata map[string]interface{}) string {
	if lifecycle == "command" {
		return ""
	}
	owner := ""
	for _, key := range []string{"sandbox_session_id", "session_id", "run_id", "task_id"} {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			owner = strings.TrimSpace(value)
			break
		}
	}
	if owner == "" {
		owner = workingDir
	}
	sum := sha1.Sum([]byte(lifecycle + ":" + owner))
	suffix := hex.EncodeToString(sum[:])[:12]
	prefix := dockerNameSanitizer.ReplaceAllString(strings.ToLower(owner), "-")
	prefix = strings.Trim(prefix, "-_.")
	if prefix == "" {
		prefix = "workspace"
	}
	if len(prefix) > 32 {
		prefix = prefix[:32]
	}
	return fmt.Sprintf("nano-%s-%s-%s", lifecycle, prefix, suffix)
}

func dockerPersistentRunArgs(containerName, image string, network NetworkPolicy, env []string, mounts []Mount, workingDir string, resources ResourceLimits) []string {
	args := []string{"docker", "run", "-d", "--name", containerName}
	if network == NetworkDenied {
		args = append(args, "--network", "none")
	}
	args = append(args, dockerResourceArgs(resources)...)
	for _, entry := range env {
		args = append(args, "-e", entry)
	}
	for _, m := range mounts {
		mode := "ro"
		if m.Mode == MountReadWrite {
			mode = "rw"
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", m.HostPath, m.Path, mode))
	}
	if workingDir != "" {
		args = append(args, "-w", workingDir)
	}
	args = append(args, image, "sh", "-c", "sleep infinity")
	return args
}

func dockerExecArgs(containerName, workingDir, command string, commandArgs []string) []string {
	args := []string{"docker", "exec"}
	if workingDir != "" {
		args = append(args, "-w", workingDir)
	}
	args = append(args, containerName, command)
	args = append(args, commandArgs...)
	return args
}

// dockerPersistentScript ensures the named lifecycle container exists, starts it
// if it was stopped, then execs the requested command inside it. The script is
// passed to "sh -c" because Docker has no single CLI operation for create-or-exec.
func dockerPersistentScript(containerName string, runArgs, execArgs []string) string {
	quotedName := shellQuote(containerName)
	return fmt.Sprintf("if docker inspect %s >/dev/null 2>&1; then docker start %s >/dev/null 2>&1 || true; else %s >/dev/null; fi; exec %s",
		quotedName,
		quotedName,
		shellJoin(runArgs),
		shellJoin(execArgs),
	)
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// dockerResourceArgs maps ResourceLimits to docker run flags. Zero values
// produce no flag so we never override the daemon's defaults unless the
// operator opted in via config.
func dockerResourceArgs(r ResourceLimits) []string {
	var args []string
	if r.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", r.MemoryMB))
	}
	if r.CPU > 0 {
		args = append(args, "--cpus", strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", r.CPU), "0"), "."))
	}
	if r.PIDsLimit > 0 {
		args = append(args, "--pids-limit", fmt.Sprintf("%d", r.PIDsLimit))
	}
	return args
}
