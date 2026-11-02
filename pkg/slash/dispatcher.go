package slash

import (
	"fmt"
	"strings"

	"github.com/nano-harness/nano-agent/pkg/agentprofile"
	"github.com/nano-harness/nano-agent/pkg/llm"
)

// Result is returned by LocalDispatcher to indicate how a slash command was
// handled.
//
// Possible outcomes:
//
//   - Handled=true: the command was fully processed locally; the caller
//     should only render Message to the user.
//   - ShouldSubmit=true: the dispatcher rewrote a slash command (e.g.
//     "/reviewer ...") into an instruction that should be submitted to the
//     LLM via SubmitInput. Callers that cannot submit on the user's behalf
//     should display an error rather than silently dropping the request.
//   - Both false: the input is not a locally-handled slash command and the
//     caller should fall through to its existing pipeline (or send the raw
//     input as-is).
type Result struct {
	Handled bool
	// Message is a single block of text to display to the user. It may be
	// multi-line and is intended to be appended to the conversation pane via
	// the caller's "system message" rendering.
	Message string
	// Level provides a hint to the UI about how to render Message. Allowed
	// values: "info", "success", "warning", "error".
	Level string
	// ShouldSubmit indicates the dispatcher rewrote the input and the caller
	// should send SubmitInput to the agent/LLM in place of the original
	// input. Mutually exclusive with Handled.
	ShouldSubmit bool
	// SubmitInput is the rewritten input to submit when ShouldSubmit is
	// true. Empty when ShouldSubmit is false.
	SubmitInput string
}

// CheckpointManager abstracts the checkpoint subsystem so the dispatcher can
// use pkg/checkpoint when checkpointing is enabled while still degrading
// gracefully when callers pass nil.
type CheckpointManager interface {
	Create(reason string) (id string, err error)
	List() ([]CheckpointInfo, error)
	Restore(id string) error
}

// CheckpointInfo is a minimal projection of a checkpoint suitable for display.
type CheckpointInfo struct {
	ID         string
	CreatedAt  string
	Reason     string
	FileCount  int
	TotalBytes int64
}

// LocalDispatcher dispatches slash commands that can be answered locally
// (without a roundtrip to the backend agent or LLM). It is shared by the
// BubbleTea TUI, the tview TUI, and the binary one-shot mode so each surface
// presents the same set of supported commands with consistent output.
type LocalDispatcher struct {
	teamName     string
	cwd          string
	registry     *Registry
	checkpointer CheckpointManager
	// modelLister returns a short formatted listing of configured models. It
	// is supplied by the caller so the dispatcher does not need to import the
	// llm package directly.
	modelLister          func() string
	modelStatusGetter    func() string
	modelSwitcher        func(string) string
	modelFallbackHandler func(string) string
	modelDoctor          func(string) string
	contextStatusGetter  func() string
	doctorReporter       func() string
	eventsQuerier        func(string) string
	auditQuerier         func(string) string
	skillLister          func() string
	routinesLister       func() string
	runningStatusLister  func() string
	routinesAdder        func(string) string
	routinesRemover      func(string) string
	routinesPauser       func(string) string
	routinesResumer      func(string) string
	routinesRunner       func(string) string
	goalHandler          func(string) string
}

// NewLocalDispatcher constructs a LocalDispatcher with the given team name
// (may be empty) and working directory. Optional callbacks may be wired with
// the With* methods.
func NewLocalDispatcher(teamName, cwd string) *LocalDispatcher {
	return &LocalDispatcher{
		teamName: teamName,
		cwd:      cwd,
		registry: NewRegistry(cwd),
	}
}

// WithCheckpointer sets the checkpoint manager. Pass nil to keep the
// dispatcher in "checkpoint not enabled" mode.
func (d *LocalDispatcher) WithCheckpointer(c CheckpointManager) *LocalDispatcher {
	d.checkpointer = c
	return d
}

// WithModelLister wires a callback that returns a short summary of available
// models for /models.
func (d *LocalDispatcher) WithModelLister(f func() string) *LocalDispatcher {
	d.modelLister = f
	return d
}

func (d *LocalDispatcher) WithModelStatusGetter(f func() string) *LocalDispatcher {
	d.modelStatusGetter = f
	return d
}

func (d *LocalDispatcher) WithModelSwitcher(f func(string) string) *LocalDispatcher {
	d.modelSwitcher = f
	return d
}

func (d *LocalDispatcher) WithModelFallbackHandler(f func(string) string) *LocalDispatcher {
	d.modelFallbackHandler = f
	return d
}

func (d *LocalDispatcher) WithModelDoctor(f func(string) string) *LocalDispatcher {
	d.modelDoctor = f
	return d
}

func (d *LocalDispatcher) WithContextStatusGetter(f func() string) *LocalDispatcher {
	d.contextStatusGetter = f
	return d
}

func (d *LocalDispatcher) WithDoctorReporter(f func() string) *LocalDispatcher {
	d.doctorReporter = f
	return d
}

func (d *LocalDispatcher) WithEventsQuerier(f func(string) string) *LocalDispatcher {
	d.eventsQuerier = f
	return d
}

func (d *LocalDispatcher) WithAuditQuerier(f func(string) string) *LocalDispatcher {
	d.auditQuerier = f
	return d
}

// WithSkillLister wires a callback for /skill:list.
func (d *LocalDispatcher) WithSkillLister(f func() string) *LocalDispatcher {
	d.skillLister = f
	return d
}

// WithRoutinesLister wires a callback for /routines list.
func (d *LocalDispatcher) WithRoutinesLister(f func() string) *LocalDispatcher {
	d.routinesLister = f
	return d
}

// WithRunningStatusLister wires a callback for /routines status.
func (d *LocalDispatcher) WithRunningStatusLister(f func() string) *LocalDispatcher {
	d.runningStatusLister = f
	return d
}

func (d *LocalDispatcher) WithRoutinesAdder(f func(string) string) *LocalDispatcher {
	d.routinesAdder = f
	return d
}

func (d *LocalDispatcher) WithRoutinesRemover(f func(string) string) *LocalDispatcher {
	d.routinesRemover = f
	return d
}

func (d *LocalDispatcher) WithRoutinesPauser(f func(string) string) *LocalDispatcher {
	d.routinesPauser = f
	return d
}

func (d *LocalDispatcher) WithRoutinesResumer(f func(string) string) *LocalDispatcher {
	d.routinesResumer = f
	return d
}

func (d *LocalDispatcher) WithRoutinesRunner(f func(string) string) *LocalDispatcher {
	d.routinesRunner = f
	return d
}

func (d *LocalDispatcher) WithGoalHandler(f func(string) string) *LocalDispatcher {
	d.goalHandler = f
	return d
}

// Dispatch attempts to handle the given raw slash input locally. It returns
// Result{Handled:false} when the input is not one of the locally-handled
// commands, in which case the caller should continue with its existing
// slash-command processing pipeline.
func (d *LocalDispatcher) Dispatch(raw string) Result {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "/") {
		return Result{}
	}
	// Split head from args. The head retains its leading slash.
	headEnd := strings.IndexAny(trimmed, " \t")
	var head, args string
	if headEnd < 0 {
		head = trimmed
	} else {
		head = trimmed[:headEnd]
		args = strings.TrimSpace(trimmed[headEnd+1:])
	}
	lowerHead := strings.ToLower(head)

	switch {
	case lowerHead == "/help":
		return d.handleHelp()
	case lowerHead == "/checkpoint":
		return d.handleCheckpoint(args)
	case lowerHead == "/checkpoints":
		return d.handleCheckpoints()
	case lowerHead == "/restore":
		return d.handleRestore(args)
	case lowerHead == "/models" || (lowerHead == "/model" && (args == "" || args == "list")):
		return d.handleModels()
	case lowerHead == "/model" && (args == "use" || strings.HasPrefix(args, "use ")):
		return d.handleModelUse(strings.TrimSpace(strings.TrimPrefix(args, "use")))
	case lowerHead == "/model" && args == "status":
		return d.handleModelStatus()
	case lowerHead == "/model" && (args == "fallback" || strings.HasPrefix(args, "fallback ")):
		return d.handleModelFallback(strings.TrimSpace(strings.TrimPrefix(args, "fallback")))
	case lowerHead == "/model" && (args == "doctor" || strings.HasPrefix(args, "doctor ")):
		return d.handleModelDoctor(strings.TrimSpace(strings.TrimPrefix(args, "doctor")))
	case lowerHead == "/context" && args == "status":
		return d.handleContextStatus()
	case lowerHead == "/goal":
		return d.handleGoal(args)
	case lowerHead == "/doctor":
		return d.handleDoctor()
	case lowerHead == "/events":
		return d.handleEvents(args)
	case lowerHead == "/audit":
		return d.handleAudit(args)
	case lowerHead == "/agents" || lowerHead == "/teammates" ||
		strings.HasPrefix(lowerHead, "/agents:") || strings.HasPrefix(lowerHead, "/teammates:"):
		return d.handleAgents(trimmed)
	case strings.HasPrefix(lowerHead, "/skill:"):
		return d.handleSkill(lowerHead, args)
	case lowerHead == "/routines":
		return d.handleRoutines(args)
	case strings.HasPrefix(lowerHead, "/opsx:"):
		return d.handleOpsx(lowerHead, args)
	}

	// Agent profile slash commands ("/reviewer prompt"). Commands registered
	// with Source="agent-profile" or Source="builtin" are rewritten into an
	// LLM submission. Conflicting profile names are already skipped at
	// registration time so priority is preserved.
	cmdName := strings.TrimPrefix(head, "/")
	if cmd, ok := d.registry.Find(cmdName); ok && (cmd.Source == "agent-profile" || cmd.Source == "builtin") {
		var profile agentprofile.AgentProfile
		var found bool
		if cmd.Source == "builtin" {
			profile, found = agentprofile.GetBuiltin(cmd.Name)
		} else {
			profile, found = agentprofile.NewManager(d.cwd).Find(cmd.Name)
		}
		if found && isValidAgentSlashName(profile.Name) {
			return Result{
				ShouldSubmit: true,
				SubmitInput:  rewriteAgentFromProfile(profile, args),
			}
		}
	}
	return Result{}
}

func (d *LocalDispatcher) handleHelp() Result {
	msg := "可用快捷键与命令：\n" +
		"\n" +
		"键盘快捷键：\n" +
		"  Enter           发送消息\n" +
		"  Shift+Enter / Ctrl+J  换行\n" +
		"  Ctrl+T          切换思考块展开/收起\n" +
		"  Ctrl+Y          复制最近一次助手回复\n" +
		"  Ctrl+L          开始新会话\n" +
		"  Ctrl+Z          取消当前任务\n" +
		"  Ctrl+P          打开命令面板\n" +
		"  Ctrl+R          搜索输入历史\n" +
		"  Ctrl+F          搜索 / 转储历史到 scrollback\n" +
		"  [               转储历史到 scrollback (输入为空时)\n" +
		"  PgUp/PgDn       翻页滚动消息\n" +
		"  Tab             自动补全\n" +
		"  Shift+Tab       切换权限模式\n" +
		"  Ctrl+C          退出 (空闲时) / 取消 (任务中)\n" +
		"\n" +
		"常用斜杠命令：\n" +
		"  /help                       显示此帮助\n" +
		"  /models, /model status      模型列表与当前状态\n" +
		"  /model use <provider/id>    切换模型 (重启 TUI 生效)\n" +
		"  /context status             查看上下文窗口占用\n" +
		"  /doctor                     诊断当前会话\n" +
		"  /checkpoint [reason]        创建一个会话快照\n" +
		"  /checkpoints, /restore <id> 列出 / 恢复快照\n" +
		"  /goal <text>                设置目标 / 工作流入口\n" +
		"  /events, /audit             查询事件与审计日志\n" +
		"  /skill:list                 列出已加载的 skill\n" +
		"  /routines [list|status|...]  管理定时 routine\n" +
		"  /agents, /teammates         团队/智能体管理"
	return Result{Handled: true, Level: "info", Message: msg}
}

func (d *LocalDispatcher) handleGoal(args string) Result {
	if d.goalHandler == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️ /goal is not available in this client."}
	}
	return Result{Handled: true, Level: "info", Message: d.goalHandler(args)}
}

func (d *LocalDispatcher) handleAgents(input string) Result {
	msg := HandleAgentsCommand(input, d.teamName)
	return Result{Handled: true, Message: msg, Level: "info"}
}

func (d *LocalDispatcher) handleCheckpoint(args string) Result {
	if d.checkpointer == nil {
		return checkpointNotEnabled("/checkpoint")
	}
	id, err := d.checkpointer.Create(args)
	if err != nil {
		return Result{Handled: true, Level: "error",
			Message: fmt.Sprintf("❌ 创建快照失败：%v", err)}
	}
	return Result{Handled: true, Level: "success",
		Message: fmt.Sprintf("✅ 已创建快照 %s", id)}
}

func (d *LocalDispatcher) handleCheckpoints() Result {
	if d.checkpointer == nil {
		return checkpointNotEnabled("/checkpoints")
	}
	infos, err := d.checkpointer.List()
	if err != nil {
		return Result{Handled: true, Level: "error",
			Message: fmt.Sprintf("❌ 列出快照失败：%v", err)}
	}
	if len(infos) == 0 {
		return Result{Handled: true, Level: "info", Message: "ℹ️  暂无快照。使用 /checkpoint [reason] 创建。"}
	}
	var b strings.Builder
	b.WriteString("Checkpoints:\n")
	for _, c := range infos {
		fmt.Fprintf(&b, "  - %s  %s  files=%d  bytes=%d", c.ID, c.CreatedAt, c.FileCount, c.TotalBytes)
		if c.Reason != "" {
			fmt.Fprintf(&b, "  reason=%q", c.Reason)
		}
		b.WriteString("\n")
	}
	return Result{Handled: true, Level: "info", Message: strings.TrimRight(b.String(), "\n")}
}

func (d *LocalDispatcher) handleRestore(args string) Result {
	if d.checkpointer == nil {
		return checkpointNotEnabled("/restore")
	}
	id := strings.TrimSpace(args)
	if id == "" {
		return Result{Handled: true, Level: "error",
			Message: "用法：/restore <checkpoint-id>。使用 /checkpoints 查看可用 ID。"}
	}
	if err := d.checkpointer.Restore(id); err != nil {
		return Result{Handled: true, Level: "error",
			Message: fmt.Sprintf("❌ 恢复快照失败：%v", err)}
	}
	return Result{Handled: true, Level: "success",
		Message: fmt.Sprintf("✅ 已从快照 %s 恢复", id)}
}

func (d *LocalDispatcher) handleModels() Result {
	if d.modelLister != nil {
		return Result{Handled: true, Level: "info", Message: d.modelLister()}
	}
	var b strings.Builder
	for _, preset := range llm.KnownProviderPresets() {
		fmt.Fprintf(&b, "%s (%s)\n", preset.DisplayName, preset.ID)
		if preset.BaseURL != "" {
			fmt.Fprintf(&b, "  base_url: %s\n", preset.BaseURL)
		}
		for _, model := range preset.Models {
			fmt.Fprintf(&b, "  - %s/%s", preset.ID, model.ID)
			if model.Capabilities.Reasoning {
				b.WriteString(" reasoning")
			}
			if model.Capabilities.Vision {
				b.WriteString(" vision")
			}
			if model.Capabilities.Embedding {
				b.WriteString(" embedding")
			}
			if model.Capabilities.LongContext {
				b.WriteString(" long-context")
			}
			b.WriteString("\n")
		}
	}
	return Result{Handled: true, Level: "info", Message: strings.TrimRight(b.String(), "\n")}
}

func (d *LocalDispatcher) handleModelUse(args string) Result {
	if d.modelSwitcher == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️  当前 TUI 未连接模型切换器。"}
	}
	return Result{Handled: true, Level: "success", Message: d.modelSwitcher(args)}
}

func (d *LocalDispatcher) handleModelStatus() Result {
	if d.modelStatusGetter == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️  当前 TUI 未连接模型状态。"}
	}
	return Result{Handled: true, Level: "info", Message: d.modelStatusGetter()}
}

func (d *LocalDispatcher) handleModelFallback(args string) Result {
	if d.modelFallbackHandler == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️  当前 TUI 未连接 fallback 管理器。"}
	}
	return Result{Handled: true, Level: "info", Message: d.modelFallbackHandler(args)}
}

func (d *LocalDispatcher) handleModelDoctor(model string) Result {
	if d.modelDoctor == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️  当前 TUI 未连接模型诊断。"}
	}
	return Result{Handled: true, Level: "info", Message: d.modelDoctor(model)}
}

func (d *LocalDispatcher) handleContextStatus() Result {
	if d.contextStatusGetter == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️  当前 TUI 未连接上下文状态。"}
	}
	return Result{Handled: true, Level: "info", Message: d.contextStatusGetter()}
}

func (d *LocalDispatcher) handleDoctor() Result {
	if d.doctorReporter == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️  当前 TUI 未连接 doctor 报告。"}
	}
	return Result{Handled: true, Level: "info", Message: d.doctorReporter()}
}

func (d *LocalDispatcher) handleEvents(args string) Result {
	if d.eventsQuerier == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️  当前 TUI 未连接事件存储。"}
	}
	return Result{Handled: true, Level: "info", Message: d.eventsQuerier(args)}
}

func (d *LocalDispatcher) handleAudit(args string) Result {
	if d.auditQuerier == nil {
		return Result{Handled: true, Level: "warning", Message: "⚠️  当前 TUI 未连接审计事件存储。"}
	}
	return Result{Handled: true, Level: "info", Message: d.auditQuerier(args)}
}

func (d *LocalDispatcher) handleSkill(head, args string) Result {
	// Recognized sub-commands: list, info, use, off, install.
	sub := strings.TrimPrefix(head, "/skill:")
	switch sub {
	case "list":
		if d.skillLister != nil {
			return Result{Handled: true, Level: "info", Message: d.skillLister()}
		}
		return Result{Handled: true, Level: "info",
			Message: "ℹ️  没有已加载的 skill。"}
	case "info", "use", "off", "install":
		if args == "" {
			return Result{Handled: true, Level: "error",
				Message: fmt.Sprintf("用法：/skill:%s <name>", sub)}
		}
		// Mutating / detail skill commands are intentionally not handled
		// locally. Falling through lets the existing agent/tool path process
		// them instead of swallowing the command in the UI.
		return Result{}
	}
	return Result{Handled: true, Level: "error",
		Message: fmt.Sprintf("❌ 未知 skill 子命令：%s", head)}
}

func (d *LocalDispatcher) handleRoutines(args string) Result {
	parts := strings.Fields(args)
	sub := "list"
	if len(parts) > 0 {
		sub = strings.ToLower(parts[0])
	}
	switch sub {
	case "list":
		if d.routinesLister != nil {
			return Result{Handled: true, Level: "info", Message: d.routinesLister()}
		}
		return Result{Handled: true, Level: "info",
			Message: "ℹ️  使用 `nano routines list` 查看定时任务。"}
	case "status":
		if d.runningStatusLister != nil {
			return Result{Handled: true, Level: "info", Message: d.runningStatusLister()}
		}
		if d.routinesLister != nil {
			return Result{Handled: true, Level: "info", Message: d.routinesLister()}
		}
		return Result{Handled: true, Level: "info",
			Message: "ℹ️  使用 `nano routines list` 查看定时任务。"}
	case "add":
		return d.handleRoutineMutation("add", strings.TrimSpace(strings.TrimPrefix(args, parts[0])), d.routinesAdder)
	case "remove":
		return d.handleRoutineMutation("remove", strings.TrimSpace(strings.TrimPrefix(args, parts[0])), d.routinesRemover)
	case "pause":
		return d.handleRoutineMutation("pause", strings.TrimSpace(strings.TrimPrefix(args, parts[0])), d.routinesPauser)
	case "resume":
		return d.handleRoutineMutation("resume", strings.TrimSpace(strings.TrimPrefix(args, parts[0])), d.routinesResumer)
	case "run":
		return d.handleRoutineMutation("run", strings.TrimSpace(strings.TrimPrefix(args, parts[0])), d.routinesRunner)
	}
	return Result{Handled: true, Level: "error",
		Message: fmt.Sprintf("❌ 未知 routines 子命令：%s", sub)}
}

func (d *LocalDispatcher) handleRoutineMutation(sub, rest string, fn func(string) string) Result {
	if fn == nil {
		return Result{Handled: true, Level: "warning",
			Message: fmt.Sprintf("⚠️  当前 TUI 未连接 routines %s 处理器。", sub)}
	}
	if strings.TrimSpace(rest) == "" {
		return Result{Handled: true, Level: "error",
			Message: fmt.Sprintf("用法：/routines %s <参数>", sub)}
	}
	return Result{Handled: true, Level: "success", Message: fn(rest)}
}

func (d *LocalDispatcher) handleOpsx(head, args string) Result {
	sub := strings.TrimPrefix(head, "/opsx:")
	if args == "" {
		return Result{Handled: true, Level: "info",
			Message: fmt.Sprintf("ℹ️  /opsx:%s 是 OpenSpec 工作流命令。需要变更名称参数：/opsx:%s <change-name>", sub, sub)}
	}
	// Let the existing OpenSpec agent/tool path execute valid workflow
	// commands. The dispatcher only provides local usage feedback for the
	// missing-argument case above.
	return Result{}
}

func checkpointNotEnabled(cmd string) Result {
	return Result{
		Handled: true,
		Level:   "warning",
		Message: fmt.Sprintf("⚠️  Checkpointing 尚未启用。%s 暂不可用。\n请参阅 docs/features/CHECKPOINTING.md 了解如何启用。", cmd),
	}
}
