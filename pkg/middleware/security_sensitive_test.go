package middleware

import (
	"testing"

	nanoconfig "github.com/nano-harness/nano-agent/pkg/config"
)

func TestSensitiveReadPath_DefaultsRequireConfirm(t *testing.T) {
	tests := []string{
		"cat .nano/nano.yaml",
		"cat ~/.ssh/id_rsa",
		"less .env.production",
		"grep -r KEY ~/.aws/",
		"grep --file=/etc/shadow KEY",
		"cat /etc/shadow",
	}

	analyzer := DefaultSemanticAnalyzer()
	for _, command := range tests {
		t.Run(command, func(t *testing.T) {
			decision, err := analyzer.Analyze(command)
			if err != nil {
				t.Fatalf("Analyze returned error: %v", err)
			}
			if decision.Action != ActionConfirm {
				t.Fatalf("expected ActionConfirm, got %v (%s)", decision.Action, decision.Reason)
			}
			if decision.Rule != "SensitiveReadPath" {
				t.Fatalf("expected SensitiveReadPath rule, got %q", decision.Rule)
			}
		})
	}
}

func TestSensitiveReadPath_NormalReadAllows(t *testing.T) {
	analyzer := DefaultSemanticAnalyzer()
	decision, err := analyzer.Analyze("cat README.md")
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("expected ActionAllow, got %v (%s)", decision.Action, decision.Reason)
	}
}

func TestArbitraryExec_DefaultsRequireConfirm(t *testing.T) {
	tests := []string{
		`python3 -c "import os; print(os.environ)"`,
		`node -e "process.env"`,
		`bash -c "rm -rf /"`,
	}

	analyzer := DefaultSemanticAnalyzer()
	for _, command := range tests {
		t.Run(command, func(t *testing.T) {
			decision, err := analyzer.Analyze(command)
			if err != nil {
				t.Fatalf("Analyze returned error: %v", err)
			}
			if decision.Action != ActionConfirm {
				t.Fatalf("expected ActionConfirm, got %v (%s)", decision.Action, decision.Reason)
			}
			if decision.Rule != "ArbitraryExec" {
				t.Fatalf("expected ArbitraryExec rule, got %q", decision.Rule)
			}
		})
	}
}

func TestArbitraryExec_VersionAllows(t *testing.T) {
	analyzer := DefaultSemanticAnalyzer()
	decision, err := analyzer.Analyze("python --version")
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("expected ActionAllow, got %v (%s)", decision.Action, decision.Reason)
	}
}

func TestSensitiveReadPath_EnvConfiguredPathRequiresConfirm(t *testing.T) {
	t.Setenv("NANO_SENSITIVE_READ_PATHS", "/tmp/secret")
	cfg, err := nanoconfig.LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	analyzer := DefaultSemanticAnalyzerWithConfig("", cfg.SensitiveReadPaths, cfg.ArbitraryExecCommands)
	decision, err := analyzer.Analyze("cat /tmp/secret/foo")
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if decision.Action != ActionConfirm {
		t.Fatalf("expected ActionConfirm, got %v (%s)", decision.Action, decision.Reason)
	}
	if decision.Rule != "SensitiveReadPath" {
		t.Fatalf("expected SensitiveReadPath rule, got %q", decision.Rule)
	}
}
