//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/event"
	"github.com/nano-harness/nano-agent/pkg/llm"
	agentTools "github.com/nano-harness/nano-agent/pkg/tools/agent"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// AgentTestSuite 是所有 e2e 集成测试的基础 Suite。
//
// 职责：
// - 统一的工作目录/Agent/Config/MockServer 初始化与清理
// - 提供 RunAgent / RunAgentWithImages 等辅助方法
// - 自动收集所有 StreamEvent 到 Events 字段，供断言使用。
type AgentTestSuite struct {
	suite.Suite

	MockServer *EnhancedMockServer
	Agent      *agent.Agent
	Config     *config.Config
	WorkDir    string

	// 事件收集
	Events  []event.StreamEvent
	eventMu sync.Mutex

	originalDir string
}

// SetupTest 在每个测试用例前执行，初始化临时工作目录、MockServer、Config 和 Agent。
func (s *AgentTestSuite) SetupTest() {
	t := s.T()

	// 1. 创建临时工作目录并切换过去
	workDir, err := os.MkdirTemp("", "nano-e2e-*")
	require.NoError(t, err, "failed to create temp workdir")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "failed to get current working dir")
	err = os.Chdir(workDir)
	require.NoError(t, err, "failed to chdir to temp workdir")

	s.WorkDir = workDir
	s.originalDir = originalDir

	// 2. 启动增强版 Mock LLM Server
	s.MockServer = NewEnhancedMockServer()

	// 3. 构造配置并指向 MockServer
	cfg := config.DefaultConfig()
	cfg.APIKey = "e2e-test-key"
	cfg.BaseURL = s.MockServer.URL()
	cfg.Model = "mock-gpt-4"

	// Turn config: lightweight setup compatible with legacy framework semantics
	cfg.Turn = &config.TurnExecutionConfig{
		// Empty - using implicit completion based on finish_reason
	}

	// LoopDetection 默认开启
	cfg.LoopDetection = &config.LoopDetectionConfig{Enabled: true}

	// UserInfo：保持原有默认，但工作目录改为当前 temp dir
	cfg.UserInfo = &config.UserInfoConfig{
		Timezone:           "UTC",
		OperatingSystem:    "test",
		Shell:              "/bin/sh",
		Editor:             "nano",
		Language:           "en",
		ProgrammingTools:   map[string]string{},
		WorkingDirectory:   workDir,
		AutoDetectUserInfo: false,
	}

	// 将持久化状态文件指向临时目录，避免污染本机 ~/.nano/state.json
	cfg.Scheduler = &config.SchedulerConfig{
		Enabled:   true,
		StateFile: filepath.Join(workDir, "state.json"),
	}

	// 为避免访问外部网络，将 OSS/MCP/Skills 等高风险集成功能关闭
	if cfg.OSS != nil {
		cfg.OSS.Enabled = false
	}
	cfg.EnableMCP = false
	if cfg.Skills != nil {
		cfg.Skills.Enabled = false
	}

	// 注入短退避 CircuitBreaker 配置，避免测试被长重试拖死
	if cfg.Advanced == nil {
		cfg.Advanced = &config.AdvancedConfig{}
	}
	cfg.Advanced.CircuitBreaker = &config.CircuitBreakerAdvConfig{
		MaxRetries:          2,
		BaseDelayMs:         50,
		MaxDelayMs:          200,
		OpenTimeoutMs:       500,
		ExcludeNonFailback:  true,
		TruncationDetection: true,
	}

	// 更新全局配置，确保 llm/client 和其他组件使用一致的配置
	config.SetGlobalConfig(cfg)

	// 4. 初始化 Agent，默认允许所有工具执行（无确认）
	agentInstance, err := agent.New(cfg)
	require.NoError(t, err, "failed to initialize agent")
	agentInstance.SetApprovalHandlerV2(func(_ *agent.ToolCallInfo) agent.ApprovalDecision {
		return agent.ApprovalApproveOnce
	})

	// E2E 测试不走 Engine，必须手动注册 agent tools (task 等)
	agentTools.RegisterAgentTools(agentInstance.GetToolbox(), cfg)

	s.Agent = agentInstance
	s.Config = cfg
	s.Events = nil
}

// TearDownTest 在每个测试用例后执行，释放资源并清理工作目录。
func (s *AgentTestSuite) TearDownTest() {
	// 优先尝试关闭 Agent
	if s.Agent != nil {
		if err := s.Agent.Shutdown(); err != nil {
			// 使用 Testing.T 而不是 panic，避免影响其它测试
			s.T().Logf("agent shutdown error: %v", err)
		}
		s.Agent = nil
	}

	// 关闭 MockServer
	if s.MockServer != nil {
		s.MockServer.Close()
		s.MockServer = nil
	}

	// 恢复工作目录
	if s.originalDir != "" {
		if err := os.Chdir(s.originalDir); err != nil {
			s.T().Logf("failed to chdir back to original dir: %v", err)
		}
	}

	// 删除临时工作目录
	if s.WorkDir != "" {
		if err := os.RemoveAll(s.WorkDir); err != nil {
			s.T().Logf("failed to remove temp workdir: %v", err)
		}
		s.WorkDir = ""
	}
}

// RunAgent 使用新的随机 sessionID 运行一次 Agent，会自动收集并返回本次 run 的所有事件。
func (s *AgentTestSuite) RunAgent(prompt string) ([]event.StreamEvent, error) {
	return s.runAgentInternal(prompt, nil)
}

// RunAgentWithImages 支持多模态输入。
func (s *AgentTestSuite) RunAgentWithImages(prompt string, images []llm.MultimodalImage) ([]event.StreamEvent, error) {
	return s.runAgentInternal(prompt, images)
}

func (s *AgentTestSuite) runAgentInternal(prompt string, images []llm.MultimodalImage) ([]event.StreamEvent, error) {
	require.NotNil(s.T(), s.Agent, "agent must be initialized in SetupTest")

	// 为本次执行重置事件收集器
	s.eventMu.Lock()
	s.Events = nil
	s.eventMu.Unlock()

	ctx := context.Background()
	sessionID := fmt.Sprintf("test_session_%d", time.Now().UnixNano())

	var (
		events   []event.StreamEvent
		eventsMu sync.Mutex
	)
	onEvent := func(ev event.StreamEvent) {
		s.eventMu.Lock()
		s.Events = append(s.Events, ev)
		s.eventMu.Unlock()

		eventsMu.Lock()
		events = append(events, ev)
		eventsMu.Unlock()
	}

	if images != nil {
		err := s.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, prompt, images, onEvent)
		return events, err
	}

	err := s.Agent.ProcessStreamWithMultimodalAndSession(ctx, sessionID, prompt, nil, onEvent)
	return events, err
}

// CreateFile 在当前工作目录下创建文件（必要时自动创建父目录）。
func (s *AgentTestSuite) CreateFile(path, content string) {
	t := s.T()
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(s.WorkDir, path)
	}

	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	err := os.WriteFile(fullPath, []byte(content), 0o644)
	require.NoError(t, err, "failed to create file %s", fullPath)
}

// ReadFile 读取当前工作目录下的文件内容。
func (s *AgentTestSuite) ReadFile(path string) (string, error) {
	fullPath := path
	if !filepath.IsAbs(path) {
		fullPath = filepath.Join(s.WorkDir, path)
	}
	b, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// AppendEvents 允许测试手动注入事件（少数场景可能需要）。
func (s *AgentTestSuite) AppendEvents(events ...event.StreamEvent) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	s.Events = append(s.Events, events...)
}
