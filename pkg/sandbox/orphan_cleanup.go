package sandbox

import (
	"context"
	"os/exec"
	"strings"
)

// CleanupOrphanContainers removes any nano-* lifecycle containers that no
// longer correspond to a live nano-agent session. It is intended to be called
// from daemon startup / shutdown so that crashes do not leak containers.
//
// activeNames is the set of container names known to be in use right now;
// any nano-* container not in that set is destroyed.
//
// CleanupOrphanContainers is best-effort: the docker CLI may be unavailable
// (returns nil) and individual rm errors are ignored.
func CleanupOrphanContainers(ctx context.Context, activeNames map[string]bool) ([]string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{.Names}}")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var removed []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if !strings.HasPrefix(name, "nano-task-") && !strings.HasPrefix(name, "nano-session-") {
			continue
		}
		if activeNames[name] {
			continue
		}
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()
		removed = append(removed, name)
	}
	return removed, nil
}
