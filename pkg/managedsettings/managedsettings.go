// Package managedsettings provides enterprise-managed configuration overrides.
//
// Managed settings are placed at OS-specific system paths by IT and override
// any user/project configuration. The precedence chain is:
//
//	managed > project (.nano.yaml) > user (~/.config/nano/config.yaml) > defaults
//
// Operators are expected to deploy this file via MDM/Group Policy. Nano always
// loads it read-only and reports its presence in `nano doctor`.
package managedsettings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// EnvOverridePath is the environment variable used to override the managed-
// settings path. Intended for tests and headless CI; production deployments
// should rely on the well-known OS-specific paths.
const EnvOverridePath = "NANO_MANAGED_SETTINGS_PATH"

// ErrNotFound is returned when no managed-settings file exists at the
// expected path. Callers must treat this as benign (no-op).
var ErrNotFound = errors.New("managed-settings: file not found")

// DefaultPath returns the OS-specific managed-settings file path, honouring
// NANO_MANAGED_SETTINGS_PATH when set.
func DefaultPath() string {
	if env := os.Getenv(EnvOverridePath); env != "" {
		return env
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/NanoAgent/managed-settings.yaml"
	case "windows":
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "NanoAgent", "managed-settings.yaml")
	default:
		return "/etc/nano-agent/managed-settings.yaml"
	}
}

// Load reads and parses the managed-settings file at path. Returns
// (nil, ErrNotFound) when the file does not exist; the caller is responsible
// for treating that as a non-error.
func Load(path string) (map[string]interface{}, error) {
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read managed-settings: %w", err)
	}
	out := make(map[string]interface{})
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse managed-settings: %w", err)
	}
	return out, nil
}

// Merge deep-merges managed into target. Maps are merged recursively; scalars
// and slices in managed override the corresponding key in target. The merged
// map is returned (target may be modified in place).
func Merge(target, managed map[string]interface{}) map[string]interface{} {
	if target == nil {
		target = make(map[string]interface{})
	}
	for k, v := range managed {
		if existing, ok := target[k]; ok {
			if em, ok := existing.(map[string]interface{}); ok {
				if vm, ok := v.(map[string]interface{}); ok {
					target[k] = Merge(em, vm)
					continue
				}
			}
		}
		target[k] = v
	}
	return target
}
