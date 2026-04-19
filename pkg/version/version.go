// Package version holds build-time version information for the nano binary.
// Variables are injected at link time via -ldflags:
//
//	-X github.com/nano-harness/nano-agent/pkg/version.Version=v1.2.3
//	-X github.com/nano-harness/nano-agent/pkg/version.BuildTime=2024-01-01_00:00:00
//	-X github.com/nano-harness/nano-agent/pkg/version.CommitHash=abc1234
package version

// Version is the semantic version of the binary, injected at build time.
// Falls back to "dev" when built without ldflags (e.g. `go run`).
var Version = "dev"

// BuildTime is the UTC timestamp when the binary was compiled.
var BuildTime = "unknown"

// CommitHash is the short git commit SHA used to build the binary.
var CommitHash = "unknown"
