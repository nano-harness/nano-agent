package main //nolint:revive

import (
	"os"

	"github.com/nano-harness/nano-agent/pkg/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
