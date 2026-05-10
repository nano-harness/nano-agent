package permission

import (
	"testing"
)

func TestCheckCommand(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		wantMatch bool
		wantLevel Severity
	}{
		// High severity tests
		{
			name:      "rm -rf on root",
			command:   "rm -rf /",
			wantMatch: true,
			wantLevel: SeverityHigh,
		},
		{
			name:      "rm -fr on home",
			command:   "rm -fr ~/",
			wantMatch: true,
			wantLevel: SeverityHigh,
		},
		{
			name:      "fork bomb",
			command:   ":(){ :|:& };:",
			wantMatch: true,
			wantLevel: SeverityHigh,
		},
		{
			name:      "dd to disk",
			command:   "dd if=/dev/zero of=/dev/sda",
			wantMatch: true,
			wantLevel: SeverityHigh,
		},
		{
			name:      "mkfs formatting",
			command:   "mkfs.ext4 /dev/sda1",
			wantMatch: true,
			wantLevel: SeverityHigh,
		},
		{
			name:      "chmod 777",
			command:   "chmod -R 777 /var/www",
			wantMatch: true,
			wantLevel: SeverityHigh,
		},

		// Medium severity tests
		{
			name:      "git force push",
			command:   "git push --force origin main",
			wantMatch: true,
			wantLevel: SeverityMedium,
		},
		{
			name:      "git push to master",
			command:   "git push origin master",
			wantMatch: true,
			wantLevel: SeverityMedium,
		},
		{
			name:      "git reset hard",
			command:   "git reset --hard origin/main",
			wantMatch: true,
			wantLevel: SeverityMedium,
		},
		{
			name:      "git clean force",
			command:   "git clean -fd",
			wantMatch: true,
			wantLevel: SeverityMedium,
		},
		{
			name:      "sudo rm",
			command:   "sudo rm -rf /tmp/test",
			wantMatch: true,
			wantLevel: SeverityHigh, // Matches rm -rf pattern (high) before sudo pattern (medium)
		},

		// Low severity tests
		{
			name:      "apt remove",
			command:   "apt remove -y nginx",
			wantMatch: true,
			wantLevel: SeverityLow,
		},
		{
			name:      "npm uninstall global",
			command:   "npm uninstall -g typescript",
			wantMatch: true,
			wantLevel: SeverityLow,
		},

		// Safe commands (should not match)
		{
			name:      "safe rm",
			command:   "rm test.txt",
			wantMatch: false,
		},
		{
			name:      "git status",
			command:   "git status",
			wantMatch: false,
		},
		{
			name:      "ls command",
			command:   "ls -la",
			wantMatch: false,
		},
		{
			name:      "cat file",
			command:   "cat README.md",
			wantMatch: false,
		},
		{
			name:      "npm install",
			command:   "npm install",
			wantMatch: false,
		},
		{
			name:      "git commit",
			command:   "git commit -m 'message'",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, matched := CheckCommand(tt.command)

			if matched != tt.wantMatch {
				t.Errorf("CheckCommand() matched = %v, want %v", matched, tt.wantMatch)
			}

			if matched && rule != nil {
				if rule.Severity != tt.wantLevel {
					t.Errorf("CheckCommand() severity = %v, want %v", rule.Severity, tt.wantLevel)
				}
				t.Logf("Matched rule: %s (severity: %s, category: %s)", rule.Reason, rule.Severity, rule.Category)
			}
		})
	}
}

func TestIsSensitiveFile(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantSensitive bool
	}{
		{
			name:          ".env file",
			path:          ".env",
			wantSensitive: true,
		},
		{
			name:          ".env.local file",
			path:          ".env.local",
			wantSensitive: true,
		},
		{
			name:          "private key",
			path:          "id_rsa",
			wantSensitive: true,
		},
		{
			name:          "credentials file",
			path:          "aws_credentials.json",
			wantSensitive: true,
		},
		{
			name:          "password file",
			path:          "passwords.txt",
			wantSensitive: true,
		},
		{
			name:          "normal file",
			path:          "main.go",
			wantSensitive: false,
		},
		{
			name:          "readme",
			path:          "README.md",
			wantSensitive: false,
		},
		{
			name:          "config file",
			path:          "config.yaml",
			wantSensitive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSensitiveFile(tt.path)
			if got != tt.wantSensitive {
				t.Errorf("IsSensitiveFile(%q) = %v, want %v", tt.path, got, tt.wantSensitive)
			}
		})
	}
}

func TestFirewallHook(t *testing.T) {
	config := FirewallConfig{
		Enabled:           true,
		SeverityThreshold: SeverityMedium,
		FailurePolicy:     "confirm",
	}

	hook := NewFirewallHook(config)

	tests := []struct {
		name       string
		toolName   string
		params     map[string]interface{}
		wantAction string // "allow", "confirm", or "block"
	}{
		{
			name:     "dangerous rm -rf",
			toolName: "run_shell_command",
			params: map[string]interface{}{
				"command": "rm -rf /tmp/test",
			},
			wantAction: "confirm",
		},
		{
			name:     "safe ls command",
			toolName: "run_shell_command",
			params: map[string]interface{}{
				"command": "ls -la",
			},
			wantAction: "allow",
		},
		{
			name:     "git force push",
			toolName: "bash",
			params: map[string]interface{}{
				"command": "git push --force origin main",
			},
			wantAction: "confirm",
		},
		{
			name:     "non-shell tool",
			toolName: "read_file",
			params: map[string]interface{}{
				"file_path": "test.txt",
			},
			wantAction: "allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := hook.Execute(nil, "pre_tool_use", tt.toolName, tt.params)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}

			var gotAction string
			switch decision.Action {
			case 0: // ActionAllow
				gotAction = "allow"
			case 1: // ActionConfirm
				gotAction = "confirm"
			case 2: // ActionBlock
				gotAction = "block"
			}

			if gotAction != tt.wantAction {
				t.Errorf("Execute() action = %v, want %v (reason: %s)", gotAction, tt.wantAction, decision.Reason)
			}
		})
	}
}
