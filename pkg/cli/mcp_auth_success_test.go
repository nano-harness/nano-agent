package cli

import (
	"os"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/config"
)

func TestEmitAuthSuccessNotificationNilConfig(t *testing.T) {
	// Must not panic when cfg is nil.
	emitAuthSuccessNotification(nil, "test-server", "read write", time.Now())
}

func TestEmitAuthSuccessNotificationNoHooks(t *testing.T) {
	// Must not panic when hooks are nil.
	cfg := &config.Config{}
	emitAuthSuccessNotification(cfg, "test-server", "read write", time.Now())
}

func TestEmitAuthSuccessNotificationEmptyEvents(t *testing.T) {
	// Must not panic when hooks exist but events are empty.
	cfg := &config.Config{
		Hooks: &config.HooksConfig{
			Events: map[string][]config.HookCommand{},
		},
	}
	emitAuthSuccessNotification(cfg, "test-server", "read write", time.Now())
}

func TestEmitAuthSuccessNotificationFiresHook(t *testing.T) {
	// Configure a Notification hook that writes a marker file.
	dir := t.TempDir()
	marker := dir + "/auth_success_fired"
	cfg := &config.Config{
		Hooks: &config.HooksConfig{
			Events: map[string][]config.HookCommand{
				"Notification": {
					{
						Matcher: "auth_success",
						Command: "touch " + marker,
						Timeout: 5,
					},
				},
			},
		},
	}
	emitAuthSuccessNotification(cfg, "test-server", "read write", time.Now().Add(24*time.Hour))

	// Check that the marker file was created.
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("auth_success hook did not fire: marker %q not found: %v", marker, err)
	}
}
