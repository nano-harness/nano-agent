package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/daemon"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/logger"
	nanoruntime "github.com/nano-harness/nano-agent/pkg/runtime"
	"github.com/nano-harness/nano-agent/pkg/slash"
	"github.com/nano-harness/nano-agent/pkg/swarm"
	"github.com/nano-harness/nano-agent/pkg/ui"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	"github.com/spf13/cobra"
)

// NewChatCommand creates the chat command for team-lead REPL
func NewChatCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start a long-running team-lead REPL",
		Long: `Start an interactive team-lead session with mailbox support.

This command launches a REPL (Read-Eval-Print Loop) where the agent acts as a team-lead,
capable of spawning teammates and coordinating multi-agent tasks. Messages from teammates
are automatically injected at the start of each turn.

Example:
  nano chat --team alpha`,
		RunE: runChat,
	}

	cmd.Flags().String("team", "default", "Team name for this lead session")
	cmd.Flags().Bool("daemon", false, "Use daemon-backed EventSource")
	cmd.Flags().String("session-id", "", "Session id to create or resume")
	cmd.Flags().Int64("since-seq", 0, "Resume daemon event streaming after this sequence")
	return cmd
}

func runChat(cmd *cobra.Command, args []string) error {
	ctx := signalContext()
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not initialized")
	}

	teamName, _ := cmd.Flags().GetString("team")
	teamName = strings.TrimSpace(teamName)
	if teamName == "" {
		teamName = "default"
	}
	sessionID, _ := cmd.Flags().GetString("session-id")
	if strings.TrimSpace(sessionID) == "" {
		sessionID = nanoruntime.BuildLeadSessionID(teamName, "chat")
	}
	uiMode := getUIMode(cmd)
	useDaemon, _ := cmd.Flags().GetBool("daemon")
	sinceSeq, _ := cmd.Flags().GetInt64("since-seq")

	logger.Infof("Starting team-lead REPL for team '%s'", teamName)

	var (
		src            eventsource.EventSource
		eng            *engine.Engine
		allowlistStore *permission.PersistentAllowlistStore
		cwd            string
	)
	cwd, err := os.Getwd()
	if err != nil {
		logger.Warnf("Failed to determine working directory: %v", err)
	}

	if useDaemon {
		client := createDaemonClient()
		session, err := client.CreateTeamLeadSessionWithOptions(sessionID, teamName, true)
		if err != nil {
			return fmt.Errorf("failed to create team-lead session: %w", err)
		}
		src = eventsource.NewDaemonWS(client, session.SessionID, teamName, sinceSeq)
	} else {
		// Load persistent allowlist and merge its rules into cfg so the engine
		// pre-populates the permission manager before the first turn.
		allowlistPath, _ := permission.DefaultPersistentAllowlistPath()
		allowlistStore = permission.NewPersistentAllowlistStore(allowlistPath)
		if err := allowlistStore.Load(); err != nil {
			logger.Warnf("Failed to load persistent allowlist: %v", err)
		}
		for _, raw := range allowlistStore.RulesForWorkdir(cwd) {
			cfg.AllowedRules = append(cfg.AllowedRules, raw)
		}

		approvalHandler := func(*agent.ToolCallInfo) bool { return false }
		var err error
		eng, err = engine.NewLeadEngine(cfg, approvalHandler, teamName)
		if err != nil {
			return fmt.Errorf("failed to create lead engine: %w", err)
		}
		defer func() { _ = eng.Shutdown() }()
		session := eng.Agent.GetSessionManager().GetOrCreateSession(sessionID)
		session.SetMetadata("swarm", agent.SessionMetadata{
			TeamName:   teamName,
			AgentName:  "team-lead",
			IsTeammate: false,
		})
		ctx = swarm.WithTeamLead(ctx, teamName, sessionID)
		src = eventsource.NewInProcess(eng, sessionID, eng.Agent.GetPermissionManager())
	}

	// Check for fullscreen mode flag
	useFullscreen, _ := cmd.Flags().GetBool("milktea")

	adapter, err := ui.NewFactory(ui.Config{
		APIBaseURL:    cfg.BaseURL,
		ShowBanner:    true,
		UseFullscreen: useFullscreen,
		WorkingDir:    cwd,
	}).Create(uiMode)
	if err != nil {
		return err
	}

	// Wire runtime capabilities into the BubbleTeaAdapter for in-process mode.
	// Daemon mode has no local engine/permission manager; daemon-side hooks handle
	// always-allow persistence via the Always:true field in outbound approvals.
	if btAdapter, ok := adapter.(*ui.BubbleTeaAdapter); ok && eng != nil {
		pm := eng.Agent.GetPermissionManager()
		eventStore := daemon.NewTaskEventStore(5000)
		btScheduler := agent.NewTUISchedulerFromScheduler(eng.Scheduler, eng.StateStore)
		eng.Agent.SetTUIScheduler(btScheduler)
		btAdapter.SetPermissionManager(pm)
		btAdapter.SetEngine(eng)
		btAdapter.SetTeamName(teamName)
		btAdapter.SetModelLister(slash.BuildModelLister(cfg))
		btAdapter.SetModelStatusGetter(slash.BuildModelStatusGetter(cfg))
		btAdapter.SetModelSwitcher(slash.BuildModelSwitcher(filepath.Join(cwd, ".nano.yaml")))
		btAdapter.SetModelFallbackHandler(slash.BuildModelFallbackHandler(cfg))
		btAdapter.SetModelDoctor(slash.BuildModelDoctor(cfg))
		btAdapter.SetContextStatusGetter(slash.BuildContextStatusGetter(eng.Agent))
		btAdapter.SetDoctorReporter(slash.BuildDoctorReporter(cfg))
		btAdapter.SetEventsQuerier(slash.BuildEventsQuerier(eventStore))
		btAdapter.SetAuditQuerier(slash.BuildAuditQuerier(eventStore))
		if sm := eng.Agent.GetSkillManager(); sm != nil {
			btAdapter.SetSkillLister(sm.ListSkillNames)
		}
		btAdapter.SetRoutinesLister(btScheduler.FormatTasks)
		btAdapter.SetRoutinesAdder(func(description string) string {
			id, err := btScheduler.AddRoutineFromDescription(description)
			if err != nil {
				return fmt.Sprintf("❌ 添加 routine 失败：%v", err)
			}
			return fmt.Sprintf("✅ 已添加 routine %s", id)
		})
		btAdapter.SetRoutinesRemover(func(taskID string) string {
			if err := btScheduler.RemoveTask(strings.TrimSpace(taskID)); err != nil {
				return fmt.Sprintf("❌ 删除 routine 失败：%v", err)
			}
			return fmt.Sprintf("✅ 已删除 routine %s", strings.TrimSpace(taskID))
		})
		btAdapter.SetRoutinesPauser(func(taskID string) string {
			if err := btScheduler.PauseTask(strings.TrimSpace(taskID)); err != nil {
				return fmt.Sprintf("❌ 暂停 routine 失败：%v", err)
			}
			return fmt.Sprintf("✅ 已暂停 routine %s", strings.TrimSpace(taskID))
		})
		btAdapter.SetRoutinesResumer(func(taskID string) string {
			if err := btScheduler.ResumeTask(strings.TrimSpace(taskID)); err != nil {
				return fmt.Sprintf("❌ 恢复 routine 失败：%v", err)
			}
			return fmt.Sprintf("✅ 已恢复 routine %s", strings.TrimSpace(taskID))
		})
		btAdapter.SetRoutinesRunner(func(taskID string) string {
			taskID = strings.TrimSpace(taskID)
			if err := btScheduler.Scheduler().RunTaskNow(taskID); err != nil {
				return fmt.Sprintf("❌ 触发 routine 失败：%v", err)
			}
			return fmt.Sprintf("✅ 已触发 routine %s 立即执行", taskID)
		})
		btAdapter.SetNewSessionHandler(func() string {
			return eng.Agent.StartNewSession()
		})
		btAdapter.SetAllowlistHandler(func(toolName string, params map[string]interface{}) {
			if pm != nil {
				rules := permission.BuildAllowlistRules(toolName, params)
				for _, rule := range rules {
					pm.GetSessionAllowlist().AddRule(rule)
					if allowlistStore != nil {
						if _, err := allowlistStore.AddRuleForWorkdir(cwd, rule.RawPattern); err != nil {
							logger.Warnf("Failed to persist allowlist rule %q: %v", rule.RawPattern, err)
						}
					}
				}
			}
		})
		if allowlistStore != nil {
			btAdapter.SetPersistentAllowlist(allowlistStore, cwd)
		}
		if allTools := eng.Agent.GetToolbox().List(); len(allTools) > 0 {
			names := make([]string, 0, len(allTools))
			for _, t := range allTools {
				names = append(names, t.Name())
			}
			btAdapter.SetAvailableToolNames(names)
		}
	}

	return adapter.Run(ctx, src)
}

// signalContext returns a context that is cancelled on SIGINT, SIGTERM, or SIGPIPE
func signalContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)

	go func() {
		<-sigCh
		logger.Info("Received termination signal")
		cancel()
	}()

	return ctx
}
