package managedsettings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathOSAware(t *testing.T) {
	t.Setenv(EnvOverridePath, "")
	p := DefaultPath()
	if p == "" {
		t.Fatal("DefaultPath should not be empty")
	}
}

func TestEnvOverrideTakesPrecedence(t *testing.T) {
	t.Setenv(EnvOverridePath, "/tmp/nano-managed.yaml")
	if got := DefaultPath(); got != "/tmp/nano-managed.yaml" {
		t.Fatalf("got %q, want override path", got)
	}
}

func TestLoadMissingReturnsErrNotFound(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	_, err := Load(missing)
	if err != ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestLoadAndMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed.yaml")
	contents := []byte(`
permission_mode: yolo
sandbox:
  backend: docker
  docker_lifecycle: session
policy:
  classifier:
    enabled: false
`)
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	managed, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if managed["permission_mode"] != "yolo" {
		t.Fatalf("expected yolo, got %v", managed["permission_mode"])
	}

	target := map[string]interface{}{
		"permission_mode": "default", // user value
		"sandbox": map[string]interface{}{
			"backend":     "native",
			"working_dir": "/tmp/scratch",
		},
	}
	out := Merge(target, managed)
	if out["permission_mode"] != "yolo" {
		t.Fatal("managed should override user permission_mode")
	}
	sb := out["sandbox"].(map[string]interface{})
	if sb["backend"] != "docker" {
		t.Fatal("managed should override sandbox.backend")
	}
	if sb["working_dir"] != "/tmp/scratch" {
		t.Fatal("user keys not overridden by managed should be preserved")
	}
	if sb["docker_lifecycle"] != "session" {
		t.Fatal("managed-only nested keys should be added")
	}
}
