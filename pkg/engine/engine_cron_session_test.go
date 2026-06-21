package engine_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/stretchr/testify/require"
)

func cronTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := minimalTestConfig()
	cfg.Cron = &config.CronConfig{
		EventsDir:          t.TempDir(),
		PermissionPolicy:   "auto_approve",
		TurnTimeout:        time.Second,
		LogRetentionDays:   30,
		LogCleanupInterval: time.Hour,
	}
	cfg.Scheduler = &config.SchedulerConfig{
		Enabled:   true,
		StateFile: filepath.Join(t.TempDir(), "state.json"),
	}
	return cfg
}

func TestExecuteTaskWithMeta_FixedSessionID(t *testing.T) {
	cfg := cronTestConfig(t)
	mock := llm.NewMockClient()
	eng, err := engine.New(cfg, nil, engine.WithScheduler(), engine.WithAgentOpts(agent.WithLLMClient(mock)))
	require.NoError(t, err)
	eng.Agent.GetSessionManager().SetStorage(nil)
	defer eng.Shutdown()

	task, err := eng.Scheduler.ScheduleTask("0 * * * * *", "remember banana")
	require.NoError(t, err)
	require.NoError(t, eng.Scheduler.RunTaskNow(task.ID))
	require.NoError(t, eng.Scheduler.RunTaskNow(task.ID))

	wantSession := "cron-task-" + task.ID
	cronSession, ok := eng.Agent.GetSessionManager().GetSession(wantSession)
	require.True(t, ok)
	require.NotEmpty(t, cronSession.GetConversationHistory())
	require.FileExists(t, filepath.Join(cfg.Cron.EventsDir, task.ID, wantSession+".jsonl"))
}

func TestExecuteTaskWithMeta_DoesNotPolluteActiveSession(t *testing.T) {
	cfg := cronTestConfig(t)
	mock := llm.NewMockClient()
	eng, err := engine.New(cfg, nil, engine.WithScheduler(), engine.WithAgentOpts(agent.WithLLMClient(mock)))
	require.NoError(t, err)
	eng.Agent.GetSessionManager().SetStorage(nil)
	defer eng.Shutdown()

	eng.Agent.SetActiveSessionID("main")
	mainSession := eng.Agent.GetSessionManager().GetOrCreateSession("main")
	before := len(mainSession.GetConversationHistory())

	task, err := eng.Scheduler.ScheduleTask("0 * * * * *", "cron command")
	require.NoError(t, err)
	require.NoError(t, eng.Scheduler.RunTaskNow(task.ID))

	after := len(mainSession.GetConversationHistory())
	require.Equal(t, before, after)
	_, ok := eng.Agent.GetSessionManager().GetSession("cron-task-" + task.ID)
	require.True(t, ok)
}

func TestExecuteTaskWithMeta_NotifierCalledStartedFinished(t *testing.T) {
	cfg := cronTestConfig(t)
	mock := llm.NewMockClient()
	eng, err := engine.New(cfg, nil, engine.WithScheduler(), engine.WithAgentOpts(agent.WithLLMClient(mock)))
	require.NoError(t, err)
	eng.Agent.GetSessionManager().SetStorage(nil)
	defer eng.Shutdown()

	var events []event.StreamEvent
	eng.SetCronNotifier(func(ev event.StreamEvent) {
		events = append(events, ev)
	})

	task, err := eng.Scheduler.ScheduleTask("0 * * * * *", "cron command")
	require.NoError(t, err)
	require.NoError(t, eng.Scheduler.RunTaskNow(task.ID))

	require.Len(t, events, 2)
	require.Equal(t, event.EventTypeCronTaskStarted, events[0].Type)
	require.Equal(t, event.EventTypeCronTaskFinished, events[1].Type)
	for _, ev := range events {
		require.Equal(t, task.ID, ev.Metadata["task_id"])
		require.Equal(t, "cron command", ev.Metadata["task_command"])
		require.Equal(t, "cron-task-"+task.ID, ev.Metadata["task_session_id"])
	}
	require.Equal(t, true, events[1].Metadata["success"])
}

func TestExecuteTaskWithMeta_FinishedHasError(t *testing.T) {
	cfg := cronTestConfig(t)
	mock := llm.NewMockClient()
	mock.Responses = []llm.MockResponse{{Error: errors.New("boom")}}
	eng, err := engine.New(cfg, nil, engine.WithScheduler(), engine.WithAgentOpts(agent.WithLLMClient(mock)))
	require.NoError(t, err)
	eng.Agent.GetSessionManager().SetStorage(nil)
	defer eng.Shutdown()

	var events []event.StreamEvent
	eng.SetCronNotifier(func(ev event.StreamEvent) {
		events = append(events, ev)
	})

	task, err := eng.Scheduler.ScheduleTask("0 * * * * *", "cron command")
	require.NoError(t, err)
	require.NoError(t, eng.Scheduler.RunTaskNow(task.ID))

	require.Len(t, events, 2)
	finished := events[1]
	require.Equal(t, event.EventTypeCronTaskFinished, finished.Type)
	require.Equal(t, false, finished.Metadata["success"])
	require.NotEmpty(t, finished.Metadata["error"])
}
