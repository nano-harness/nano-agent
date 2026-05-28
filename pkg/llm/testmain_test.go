package llm

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	os.Setenv("NANO_USER_CONFIG", "") // isolate tests from user's global config
	os.Exit(m.Run())
}
