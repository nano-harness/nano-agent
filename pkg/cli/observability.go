package cli

import (
	"encoding/json"
	"fmt"

	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/spf13/cobra"
)

func NewEventsCommand() *cobra.Command {
	return newEventQueryCommand("events", "Query daemon events", false, nil)
}

func NewAuditCommand() *cobra.Command {
	return newEventQueryCommand("audit", "Query daemon audit events", true, nil)
}

func newEventQueryCommand(use, short string, auditOnly bool, factory ClientFactory) *cobra.Command {
	if factory == nil {
		factory = createDaemonClient
	}
	var query daemon.EventQuery
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(_ *cobra.Command, _ []string) error {
			client := factory()
			var (
				resp *daemon.EventQueryResponse
				err  error
			)
			if auditOnly {
				resp, err = client.QueryAudit(query)
			} else {
				resp, err = client.QueryEvents(query)
			}
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
	cmd.Flags().StringVar(&query.SessionID, "session-id", "", "Filter by session ID")
	cmd.Flags().StringVar(&query.RunID, "run-id", "", "Filter by run ID")
	cmd.Flags().StringVar(&query.Type, "type", "", "Filter by event type")
	cmd.Flags().BoolVar(&query.Sandbox, "sandbox", false, "Only include sandbox events")
	cmd.Flags().Int64Var(&query.SinceSeq, "since-seq", 0, "Only include events after this sequence")
	cmd.Flags().IntVar(&query.Limit, "limit", 200, "Maximum events to return")
	return cmd
}

func NewDoctorCommand() *cobra.Command {
	return newDoctorCommand(nil)
}

func newDoctorCommand(factory ClientFactory) *cobra.Command {
	if factory == nil {
		factory = createDaemonClient
	}
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Inspect local configuration and daemon observability health",
		RunE: func(_ *cobra.Command, _ []string) error {
			report := buildDoctorReport(factory)
			if outputJSON {
				data, _ := json.MarshalIndent(report, "", "  ")
				fmt.Println(string(data))
				return nil
			}
			fmt.Printf("config_loaded: %t\n", report["config_loaded"])
			fmt.Printf("sandbox_backend: %v\n", report["sandbox_backend"])
			fmt.Printf("sandbox_enabled: %v\n", report["sandbox_enabled"])
			fmt.Printf("audit_log: %v\n", report["audit_log"])
			fmt.Printf("daemon_reachable: %v\n", report["daemon_reachable"])
			if errText, ok := report["daemon_error"].(string); ok && errText != "" {
				fmt.Printf("daemon_error: %s\n", errText)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print JSON output")
	return cmd
}

func buildDoctorReport(factory ClientFactory) map[string]any {
	cfg := config.Get()
	report := map[string]any{
		"config_loaded":     cfg != nil,
		"sandbox_enabled":   nil,
		"sandbox_backend":   "",
		"audit_log":         "",
		"daemon_reachable":  false,
		"daemon_status":     nil,
		"daemon_error":      "",
		"observability_api": []string{"/api/v1/events", "/api/v1/audit"},
	}
	if cfg != nil {
		if cfg.Sandbox != nil {
			report["sandbox_enabled"] = cfg.Sandbox.Enabled
			report["sandbox_backend"] = cfg.Sandbox.Backend
		}
		if cfg.Middleware != nil {
			report["audit_log"] = cfg.Middleware.AuditLogPath
		}
	}
	client := factory()
	if status, err := client.Status(); err == nil {
		report["daemon_reachable"] = true
		report["daemon_status"] = status
	} else {
		report["daemon_error"] = err.Error()
	}
	return report
}
