//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSandbox_BasicCommandExecution tests basic command preparation in a Docker sandbox.
func TestSandbox_BasicCommandExecution(t *testing.T) {
	env := prepareDockerCommand(t, sandbox.SandboxRequest{Command: "echo hello world"})

	assert.Equal(t, sandbox.BackendDocker, env.Backend)
	assert.Equal(t, "docker", env.Command)
	assert.Contains(t, strings.Join(env.Args, " "), "echo hello world")
	assert.Equal(t, "alpine:latest", env.Metadata["image"])
	assert.Equal(t, "command", env.Metadata["lifecycle"])
}

// TestSandbox_CommandWithWorkingDirectory tests command preparation with custom working directory.
func TestSandbox_CommandWithWorkingDirectory(t *testing.T) {
	env := prepareDockerCommand(t, sandbox.SandboxRequest{Command: "pwd", WorkingDir: "/tmp"})

	assert.Equal(t, "/tmp", env.WorkingDir)
	assert.Contains(t, env.Args, "-w")
	assert.Contains(t, env.Args, "/tmp")
}

// TestSandbox_CommandTimeout tests timeout metadata propagation.
func TestSandbox_CommandTimeout(t *testing.T) {
	env := prepareDockerCommand(t, sandbox.SandboxRequest{
		Command:        "sleep 10",
		ResourceLimits: sandbox.ResourceLimits{Timeout: time.Second},
	})

	assert.Equal(t, time.Second, env.ResourceLimits.Timeout)
}

// TestSandbox_FileOperations tests read/write mount preparation for file operations.
func TestSandbox_FileOperations(t *testing.T) {
	workDir := t.TempDir()
	runtime := sandbox.NewRuntime(newDockerSandboxConfig(), workDir)
	env, err := runtime.PrepareCommand(context.Background(), sandbox.SandboxRequest{Command: "cat testfile.txt", WorkingDir: workDir})
	require.NoError(t, err)

	assert.NotEmpty(t, env.Mounts)
	assert.Equal(t, workDir, env.Mounts[0].HostPath)
	assert.Equal(t, sandbox.MountReadWrite, env.Mounts[0].Mode)
}

// TestSandbox_EnvironmentVariables tests NANO_* environment variable propagation.
func TestSandbox_EnvironmentVariables(t *testing.T) {
	env := prepareDockerCommand(t, sandbox.SandboxRequest{
		Command: "echo $NANO_TEST_VAR $SECRET_VAR",
		Env:     []string{"NANO_TEST_VAR=test_value_123", "SECRET_VAR=hidden"},
	})

	joined := strings.Join(env.Args, " ")
	assert.Contains(t, joined, "NANO_TEST_VAR=test_value_123")
	assert.NotContains(t, joined, "SECRET_VAR=hidden")
	assert.Contains(t, env.EnvNames, "NANO_TEST_VAR")
}

func prepareDockerCommand(t *testing.T, req sandbox.SandboxRequest) *sandbox.SandboxEnvironment {
	t.Helper()
	runtime := sandbox.NewRuntime(newDockerSandboxConfig(), t.TempDir())
	env, err := runtime.PrepareCommand(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, runtime.Cleanup(context.Background(), env))
	return env
}

func newDockerSandboxConfig() *config.SandboxConfig {
	return &config.SandboxConfig{
		Enabled:         true,
		Backend:         "docker",
		DockerImage:     "alpine:latest",
		DockerLifecycle: "command",
		NetworkAccess:   true,
	}
}
