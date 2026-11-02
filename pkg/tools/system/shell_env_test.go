package system

import (
	"strings"
	"testing"
)

func envSliceHas(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

func TestBuildEnvironment_DropsSensitiveVarsByDefault(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-test-secret")
	t.Setenv("REGULAR_VAR_NANO_TEST", "visible")

	tool := &ShellTool{}
	env := tool.buildEnvironment("")

	if envSliceHas(env, "OPENAI_API_KEY") {
		t.Error("OPENAI_API_KEY must not be inherited by shell subprocesses")
	}
	if envSliceHas(env, "AWS_SECRET_ACCESS_KEY") {
		t.Error("AWS_SECRET_ACCESS_KEY must not be inherited by shell subprocesses")
	}
	if !envSliceHas(env, "REGULAR_VAR_NANO_TEST") {
		t.Error("non-sensitive variables must pass through")
	}
}

func TestBuildEnvironment_SensitiveVarsDroppedFromExplicitEnv(t *testing.T) {
	tool := &ShellTool{}
	env := tool.buildEnvironment("OPENAI_API_KEY=sk-injected;FOO=bar")

	if envSliceHas(env, "OPENAI_API_KEY") {
		t.Error("explicitly passed sensitive key must be dropped without an allowlist exemption")
	}
	if !envSliceHas(env, "FOO") {
		t.Error("non-sensitive explicit entries must pass through")
	}
}

func TestBuildEnvironment_AllowedEnvVarsExemptsSensitive(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-secret")

	tool := &ShellTool{allowedEnvVars: []string{"openai_api_key"}} // case-insensitive
	env := tool.buildEnvironment("")

	if !envSliceHas(env, "OPENAI_API_KEY") {
		t.Error("allowed_env_vars must exempt a sensitive key from the built-in blocklist")
	}
}

func TestBuildEnvironment_BlockedVarsStillDropped(t *testing.T) {
	t.Setenv("CUSTOM_BLOCKED_NANO_TEST", "secret")

	tool := &ShellTool{blockedEnvVars: []string{"CUSTOM_BLOCKED_NANO_TEST"}}
	env := tool.buildEnvironment("CUSTOM_BLOCKED_NANO_TEST=override")

	if envSliceHas(env, "CUSTOM_BLOCKED_NANO_TEST") {
		t.Error("blocked_env_vars entries must stay dropped (base and explicit)")
	}
}
