//go:build e2e

package e2e

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nano-harness/nano-agent/pkg/cron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCron_BasicTaskExecution tests basic cron task scheduling and execution.
func TestCron_BasicTaskExecution(t *testing.T) {
	executionChan := make(chan string, 1)
	cronMgr := cron.New(func(command string) error {
		executionChan <- command
		return nil
	})
	cronMgr.Start()
	defer cronMgr.Stop()

	task, err := cronMgr.ScheduleTask("* * * * * *", "test-task-1")
	require.NoError(t, err)
	require.NotNil(t, task)

	select {
	case got := <-executionChan:
		assert.Equal(t, "test-task-1", got)
	case <-time.After(3 * time.Second):
		t.Fatal("Task did not execute within timeout")
	}

	require.NoError(t, cronMgr.RemoveTask(task.ID))
	t.Log("✓ Basic cron task execution test passed")
}

// TestCron_TaskPersistence tests that cron tasks are exposed through the scheduler task list.
func TestCron_TaskPersistence(t *testing.T) {
	cronMgr := cron.New(func(string) error { return nil })
	cronMgr.Start()
	defer cronMgr.Stop()

	task, err := cronMgr.ScheduleTask("0 0 0 * * *", "persistent-task")
	require.NoError(t, err)

	tasks := cronMgr.ListTasks()
	require.Len(t, tasks, 1)
	assert.Equal(t, task.ID, tasks[0].ID)
	assert.Equal(t, "persistent-task", tasks[0].Command)

	t.Log("✓ Cron task persistence test passed")
}

// TestCron_MultipleTaskScheduling tests scheduling multiple concurrent tasks.
func TestCron_MultipleTaskScheduling(t *testing.T) {
	executionChan := make(chan string, 10)
	cronMgr := cron.New(func(command string) error {
		executionChan <- command
		return nil
	})
	cronMgr.Start()
	defer cronMgr.Stop()

	for i := 1; i <= 3; i++ {
		_, err := cronMgr.ScheduleTask("* * * * * *", fmt.Sprintf("task-%d", i))
		require.NoError(t, err)
	}

	executions := make(map[string]int)
	deadline := time.After(5 * time.Second)
	for len(executions) < 3 {
		select {
		case taskID := <-executionChan:
			executions[taskID]++
		case <-deadline:
			t.Fatalf("timed out waiting for all tasks, got %#v", executions)
		}
	}

	assert.Len(t, cronMgr.ListTasks(), 3)
	t.Log("✓ Multiple task scheduling test passed")
}

// TestCron_TaskErrorHandling tests error handling in cron tasks.
func TestCron_TaskErrorHandling(t *testing.T) {
	executionChan := make(chan bool, 1)
	cronMgr := cron.New(func(string) error {
		executionChan <- true
		return assert.AnError
	})
	cronMgr.Start()
	defer cronMgr.Stop()

	task, err := cronMgr.ScheduleTask("* * * * * *", "error-task")
	require.NoError(t, err)

	select {
	case <-executionChan:
		t.Log("Task executed and returned error as expected")
	case <-time.After(3 * time.Second):
		t.Fatal("Task did not execute within timeout")
	}

	tasks := cronMgr.ListTasks()
	require.Len(t, tasks, 1)
	assert.Equal(t, task.ID, tasks[0].ID)
	t.Log("✓ Task error handling test passed")
}

// TestCron_RunTaskNow executes a task immediately without waiting for a schedule tick.
func TestCron_RunTaskNow(t *testing.T) {
	var mu sync.Mutex
	var commands []string
	cronMgr := cron.New(func(command string) error {
		mu.Lock()
		defer mu.Unlock()
		commands = append(commands, command)
		return nil
	})

	task, err := cronMgr.ScheduleTask("0 0 0 * * *", "manual-task")
	require.NoError(t, err)
	require.NoError(t, cronMgr.RunTaskNow(task.ID))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"manual-task"}, commands)
}
