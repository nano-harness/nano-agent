package bubbletea

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/slash"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/reflow/wordwrap"
)

// ── Semantic color palette ───────────────────────────────────────────────────
// Inspired by GitHub Copilot CLI's semantic color role design.
// Uses 256-color ANSI codes chosen for readability on dark terminal backgrounds.
const (
	colorAssistant      = "115" // Soft sage green  – AI responses
	colorUser           = "75"  // Soft blue         – user messages
	colorTool           = "249" // Light gray        – tool/secondary info
	colorError          = "203" // Soft coral red    – errors (readable, not blinding)
	colorSystem         = "179" // Warm gold         – system messages
	colorStatus         = "73"  // Soft teal         – status/token info
	colorSuccess        = "114" // Soft green        – success feedback
	colorWarning        = "215" // Soft orange       – warnings/confirmation titles
	colorMuted          = "245" // Medium gray       – separators, help text, borders
	colorDetail         = "252" // Light gray        – detail/description text
	colorBright         = "255" // Near-white        – important message text
	colorInfoTitle      = "75"  // Soft blue         – info panel titles
	colorDimBg          = "238" // Dark gray         – subtle backgrounds (slightly lighter than 236)
	colorConfirmBg      = "236" // Dark gray         – confirmation dialog title bg
	colorSecondary      = "248" // Lighter gray      – secondary/tool info text, inactive button fg
	colorDefaultFg      = "250" // Light gray        – default permission mode badge foreground
	colorYoloBg         = "208" // Deep orange       – YOLO mode badge background
	colorOpenSpec       = "135" // Soft purple       – OpenSpec category label
	colorOnAccent       = "0"   // Black             – foreground on colored badge backgrounds
	colorAcceptEditsBg  = "220" // Amber yellow      – AcceptEdits permission badge background
	colorButtonFg       = "15"  // Bright white      – confirmation dialog button foreground
	colorYesButtonBg    = "28"  // Forest green      – "confirm" button background
	colorNoButtonBg     = "124" // Dark red          – "cancel" button background
	colorAlwaysButtonBg = "33"  // Ocean blue        – "always allow" button background
)

// commandsPaletteVisibleRows is the number of command rows rendered at once in
// the command palette. Both Update() and renderCommandsPalette() must use this
// constant so the scrolling window stays consistent.
const commandsPaletteVisibleRows = 15

// -- Model --

// displayPhase represents the current content display phase
type displayPhase int

const (
	phaseIdle displayPhase = iota
	phaseThinking   // 正在展示推理内容
	phaseToolCall   // 正在展示工具调用
	phaseResponse   // 正在展示最终回复
)

type Model struct { //nolint:revive
	// Channels
	SubmitCh chan<- string
	CancelCh chan<- struct{}

	// Properties
	lines      []string
	status     string
	termWidth  int
	termHeight int
	apiBaseURL string
	cwd        string

	// History buffer management
	sessionStartTime time.Time // Session start timestamp

	// Components
	input textinput.Model

	lastRenderHeight int

	// Confirmation dialog state
	showingConfirmation  bool
	confirmationMessage  string
	confirmationToolInfo map[string]interface{}
	confirmationCallback func(bool)
	confirmationSelected int // 0 = Yes, 1 = No, 2 = Always (allowlist)

	// allowlistHandler is invoked when the user picks option 2 ("同意并不再询问").
	allowlistHandler func(toolName string, params map[string]interface{})

	// Token status (令牌细分展示)
	tokenStatus string

	showingCommands      bool
	commands             []slash.Command
	commandsIndex        int
	commandsScrollOffset int // first visible row index in the commands palette
	// slashNames caches the slash-prefixed command names for Tab completion.
	// Refreshed whenever the commands palette is opened via loadCommands().
	slashNames []string

	// Permission management
	permissionManager *permission.Manager
	permissionMode    string // cached permission mode for status bar display

	// Engine management
	engine *engine.Engine

	// Available tool names for Tab completion
	availableToolNames []string

	// Streaming text aggregation
	streamingBuf strings.Builder
	isStreaming  bool

	// Thinking block (collapsible reasoning preview)
	thinkingTitle     string
	thinkingReasoning string
	thinkingCollapsed bool
	thinkingCompleted bool

	// Input history
	inputHistory []string
	historyIndex int    // -1 means "not browsing history"
	historyDraft string // draft saved when user starts browsing history

	// Content display phase management
	currentPhase displayPhase

	// Tool call tracking for deduplication
	activeToolCalls map[string]string // ID -> last displayed status
}

func New(submitCh chan<- string, cancelCh chan<- struct{}, apiBaseURL, cwd string) *Model { //nolint:revive
	ti := textinput.New()
	ti.Placeholder = "输入您的请求..."
	ti.Focus()
	ti.CharLimit = 10000 // Generous limit to accommodate long inputs
	ti.Width = 50

	return &Model{
		SubmitCh:   submitCh,
		CancelCh:   cancelCh,
		lines:      make([]string, 0),
		status:     "等待输入",
		apiBaseURL: apiBaseURL,
		cwd:        cwd,
		input:      ti,

		// History buffer initialization
		sessionStartTime: time.Now(),
		lastRenderHeight: 0,

		thinkingCollapsed: true,

		historyIndex: -1,

		// Initialize phase and tool tracking
		currentPhase:    phaseIdle,
		activeToolCalls: make(map[string]string),
	}
}

func (m *Model) Init() tea.Cmd { //nolint:revive
	banner := `
 _   _                      _                    _
| \ | | __ _ _ __   ___    / \   __ _  ___ _ __ | |_
|  \| |/ _` + "`" + ` | '_ \ / _ \  / _ \ / _` + "`" + ` |/ _ \ '_ \| __|
| |\  | (_| | | | | (_) |/ ___ \ (_| |  __/ | | | |_
|_| \_|\__,_|_| |_|\___//_/   \_\__, |\___|_| |_|\__|
                                 |___/`
	bannerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser)).Bold(true)
	styledBanner := bannerStyle.Render(banner)

	return tea.Batch(
		textinput.Blink,
		tea.Printf("%s\n", styledBanner),
	)
}

// ClearSession clears the current session and starts fresh
func (m *Model) ClearSession() {
	// Reset session state
	m.lines = make([]string, 0)
	m.sessionStartTime = time.Now()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:revive
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.showingCommands {
			switch msg.String() {
			case "up", "k":
				if m.commandsIndex > 0 {
					m.commandsIndex--
					// Scroll up if selection moved above visible window.
					if m.commandsIndex < m.commandsScrollOffset {
						m.commandsScrollOffset = m.commandsIndex
					}
				}
				return m, nil
			case "down", "j":
				if m.commandsIndex < len(m.commands)-1 {
					m.commandsIndex++
					// Scroll down if selection moved below visible window.
					if m.commandsIndex >= m.commandsScrollOffset+commandsPaletteVisibleRows {
						m.commandsScrollOffset = m.commandsIndex - commandsPaletteVisibleRows + 1
					}
				}
				return m, nil
			case "enter":
				if len(m.commands) > 0 {
					name := m.commands[m.commandsIndex].Name
					m.input.SetValue("/" + name + " ")
				}
				m.showingCommands = false
				return m, nil
			case "esc", "q", "ctrl+c":
				m.showingCommands = false
				return m, nil
			}
			return m, nil
		}
		// Handle confirmation dialog input first
		if m.showingConfirmation {
			switch msg.String() {
			case "left", "h":
				if m.confirmationSelected > 0 {
					m.confirmationSelected--
				}
				return m, nil
			case "right", "l":
				if m.confirmationSelected < 2 {
					m.confirmationSelected++
				}
				return m, nil
			case "enter":
				// Build the confirmation command BEFORE hiding the dialog
				// (hideConfirmation clears the fields we need).
				cmd := m.buildConfirmationCmd()
				m.hideConfirmation()
				return m, cmd
			case "ctrl+c", "q", "esc":
				// Cancel confirmation – treat as "No" via async cmd.
				callback := m.confirmationCallback
				m.hideConfirmation()
				return m, func() tea.Msg {
					if callback != nil {
						callback(false)
					}
					return nil
				}
			}
			// If it's a confirmation dialog but key not handled, just return without processing
			return m, nil
		}

		// Normal input handling when not showing confirmation
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+z":
			select {
			case m.CancelCh <- struct{}{}:
			default:
			}
			return m, nil
		case "up":
			// Browse input history (most-recent first)
			if len(m.inputHistory) == 0 {
				return m, nil
			}
			if m.historyIndex == -1 {
				// Save current draft before navigating
				m.historyDraft = m.input.Value()
				m.historyIndex = len(m.inputHistory) - 1
			} else if m.historyIndex > 0 {
				m.historyIndex--
			}
			m.input.SetValue(m.inputHistory[m.historyIndex])
			m.input.CursorEnd()
			return m, nil
		case "down":
			if m.historyIndex == -1 {
				return m, nil
			}
			if m.historyIndex < len(m.inputHistory)-1 {
				m.historyIndex++
				m.input.SetValue(m.inputHistory[m.historyIndex])
			} else {
				// Restore draft
				m.historyIndex = -1
				m.input.SetValue(m.historyDraft)
			}
			m.input.CursorEnd()
			return m, nil
		case "tab":
			val := m.input.Value()
			if strings.HasPrefix(val, "/") {
				completed, suggestions := m.tabComplete(val)
				if completed != "" {
					m.input.SetValue(completed)
					m.input.CursorEnd()
				}
				if len(suggestions) > 1 {
					hint := formatLine("system", "补全候选："+strings.Join(suggestions, "  "))
					return m, tea.Printf("%s", hint)
				}
			}
			return m, nil
		case "enter":
			if m.input.Value() != "" {
				input := m.input.Value()

				// Intercept permission slash commands before forwarding to agent
				if strings.HasPrefix(input, "/") {
					if handled, cmd := m.handlePermissionSlashCommand(input); handled {
						m.input.Reset()
						m.historyIndex = -1
						return m, cmd
					}
				}

				newLine := formatLine("user", input)
				m.lines = append(m.lines, newLine)
				m.SubmitCh <- input

				// Record in history (avoid consecutive duplicates), cap at 100 entries
				if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != input {
					m.inputHistory = append(m.inputHistory, input)
					if len(m.inputHistory) > 100 {
						m.inputHistory = m.inputHistory[1:]
					}
				}
				m.historyIndex = -1
				m.historyDraft = ""

				wrapped := truncateLines(wordwrap.String(newLine, m.termWidth), m.termWidth)
				cmds = append(cmds, tea.Printf("%s", wrapped))
				m.status = "处理中..."
				m.input.Reset()
			}
		case "ctrl+p":
			m.openCommandsPalette()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.input.Width = msg.Width - 4 // Leave some margin
		// No ClearScreen – just update dimensions to preserve scrollback
		return m, nil

	case ThinkingMsg:
		// Flush any in-progress streaming buffer before rendering thinking block
		if m.isStreaming {
			cmds = append(cmds, m.flushStreamingBuffer()...)
		}

		hadBlock := m.thinkingTitle != "" || m.thinkingReasoning != ""
		prevCompleted := m.thinkingCompleted

		isComplete := false
		if msg.Metadata != nil {
			if done, ok := msg.Metadata["is_complete"].(bool); ok && done {
				isComplete = true
			}
		}
		m.thinkingCompleted = isComplete

		isNewSession := !hadBlock || (prevCompleted && !isComplete)

		// Update title
		if msg.Title != "" {
			m.thinkingTitle = msg.Title
		}

		// Update internal accumulated reasoning content
		if isNewSession {
			m.thinkingReasoning = ""
		}
		if msg.Reasoning != "" {
			m.thinkingReasoning = msg.Reasoning
		}

		// === Optimized rendering logic: compressed display ===
		if isNewSession {
			// New session: set phase and print title line only
			m.currentPhase = phaseThinking
			title := m.thinkingTitle
			if strings.TrimSpace(title) == "" {
				title = "正在思考..."
			}
			header := formatLine("thinking", fmt.Sprintf("%s [进行中]", title))
			m.lines = append(m.lines, header)
			wrapped := truncateLines(wordwrap.String(header, m.termWidth), m.termWidth)
			cmds = append(cmds, tea.Printf("%s", wrapped))
			// Do NOT print delta content - keep it compressed
		} else if isComplete {
			// Complete: print summary only (no delta)
			runes := []rune(m.thinkingReasoning)
			summary := formatLine("thinking",
				fmt.Sprintf("思考完成 [%d 字符]", len(runes)))
			m.lines = append(m.lines, summary)
			wrapped := truncateLines(wordwrap.String(summary, m.termWidth), m.termWidth)
			cmds = append(cmds, tea.Printf("%s", wrapped))
			m.currentPhase = phaseIdle
		}
		// else: streaming update - don't print anything, just update status bar

		// Update status bar
		if isComplete {
			m.status = "完成"
		} else {
			m.status = "思考中... " + m.buildThinkingPreview()
		}
		return m, tea.Batch(cmds...)

	case StatusUpdate:
		// Flush any in-progress streaming buffer before changing status
		if m.isStreaming {
			cmds = append(cmds, m.flushStreamingBuffer()...)
		}
		// Guard: if this is the delayed reset to "等待输入", only apply it when
		// the status is still "完成" (i.e. user has not submitted a new request).
		if string(msg) == "等待输入" && m.status != "完成" {
			return m, tea.Batch(cmds...)
		}
		m.status = string(msg)
		if string(msg) == "完成" {
			cmds = append(cmds, tea.Tick(time.Second, func(t time.Time) tea.Msg {
				return StatusUpdate("等待输入")
			}))
		}
		return m, tea.Batch(cmds...)

	case ToolUseMsg:
		// Flush streaming buffer before a tool call line
		if m.isStreaming {
			cmds = append(cmds, m.flushStreamingBuffer()...)
		}

		// Transition phase from thinking to tool call
		if m.currentPhase == phaseThinking {
			m.currentPhase = phaseToolCall
		}

		lastStatus := m.activeToolCalls[msg.ID]

		switch msg.Status {
		case "executing":
			if lastStatus == "" {
				// First time seeing this tool: print "正在调用"
				line := formatLine("tool", fmt.Sprintf("正在调用工具: %s", msg.ToolName))
				m.lines = append(m.lines, line)
				wrapped := truncateLines(wordwrap.String(line, m.termWidth), m.termWidth)
				cmds = append(cmds, tea.Printf("%s", wrapped))
			}
			// Otherwise skip to avoid duplicate "executing" messages
		case "success":
			if lastStatus != "success" {
				// Print completion marker (concise version); normalize whitespace in preview
				resultPreview := truncateResult(strings.ReplaceAll(msg.Result, "\n", " "), 200)
				line := formatLine("tool", fmt.Sprintf("工具: %s 调用完成\n结果: %s", msg.ToolName, resultPreview))
				m.lines = append(m.lines, line)
				wrapped := truncateLines(wordwrap.String(line, m.termWidth), m.termWidth)
				cmds = append(cmds, tea.Printf("%s", wrapped))
			}
		case "error":
			// Always print errors
			line := formatLine("error", fmt.Sprintf("工具 %s 执行失败: %s", msg.ToolName, msg.Result))
			m.lines = append(m.lines, line)
			wrapped := truncateLines(wordwrap.String(line, m.termWidth), m.termWidth)
			cmds = append(cmds, tea.Printf("%s", wrapped))
		case "cancelled":
			// Print cancellation notice unless already shown
			if lastStatus != "cancelled" {
				line := formatLine("tool", fmt.Sprintf("工具 %s 已取消", msg.ToolName))
				m.lines = append(m.lines, line)
				wrapped := truncateLines(wordwrap.String(line, m.termWidth), m.termWidth)
				cmds = append(cmds, tea.Printf("%s", wrapped))
			}
		}

		// Clean up terminal-state entries to avoid unbounded memory growth
		switch msg.Status {
		case "success", "error", "cancelled":
			delete(m.activeToolCalls, msg.ID)
		default:
			m.activeToolCalls[msg.ID] = msg.Status
		}

	case Message:
		if msg.Role == "assistant_stream" {
			// Accumulate streaming chunks; flush only when streaming ends
			if !m.isStreaming {
				m.isStreaming = true
				m.streamingBuf.Reset()
			}
			m.streamingBuf.WriteString(msg.Content)
			return m, nil
		}

		// Non-streaming message: flush any pending stream first
		if m.isStreaming {
			cmds = append(cmds, m.flushStreamingBuffer()...)
		}

		newLine := formatLine(msg.Role, msg.Content)
		m.lines = append(m.lines, newLine)
		wrapped := truncateLines(wordwrap.String(newLine, m.termWidth), m.termWidth)
		cmds = append(cmds, tea.Printf("%s", wrapped))

	case ShowConfirmationMsg:
		m.showingCommands = false // dismiss commands palette so dialog is always visible
		m.showingConfirmation = true
		m.confirmationMessage = msg.Message
		m.confirmationToolInfo = msg.ToolInfo
		m.confirmationCallback = msg.Callback
		m.confirmationSelected = 0
		return m, nil

	case TokenStatsUpdate: // Handle token stats updates
		in := formatCount(msg.InputTokens)
		out := formatCount(msg.OutputTokens)
		total := formatCount(msg.TotalTokens)
		tokens := fmt.Sprintf("输入 %s | 输出 %s | 总计 %s", in, out, total)
		if msg.Peak > 0 {
			tokens += fmt.Sprintf(" | 峰值速率: %.2f t/s", msg.Peak)
		}
		m.tokenStatus = tokens
		return m, nil
	}

	// Update input
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// buildConfirmationCmd captures the current confirmation state and returns a
// tea.Cmd that executes the user's choice in a background goroutine.
// This avoids calling the callback synchronously from Update(), which would
// trigger p.Send() while the event loop is still processing – causing a deadlock.
func (m *Model) buildConfirmationCmd() tea.Cmd {
	callback := m.confirmationCallback
	selected := m.confirmationSelected
	toolInfo := m.confirmationToolInfo
	ah := m.allowlistHandler

	return func() tea.Msg {
		switch selected {
		case 0: // 同意
			if callback != nil {
				callback(true)
			}
		case 1: // 拒绝
			if callback != nil {
				callback(false)
			}
		case 2: // 同意并不再询问
			// Capture tool info before approving to avoid races.
			toolName, _ := toolInfo["Name"].(string)
			origParams, _ := toolInfo["Parameters"].(map[string]interface{})
			var paramsCopy map[string]interface{}
			if origParams != nil {
				paramsCopy = make(map[string]interface{}, len(origParams))
				for k, v := range origParams {
					paramsCopy[k] = v
				}
			}
			if callback != nil {
				callback(true)
			}
			if ah != nil {
				ah(toolName, paramsCopy)
			}
		}
		return nil
	}
}

// flushStreamingBuffer emits the accumulated streaming content as a single
// formatted message and resets the streaming state.
func (m *Model) flushStreamingBuffer() []tea.Cmd {
	if !m.isStreaming || m.streamingBuf.Len() == 0 {
		m.isStreaming = false
		return nil
	}
	content := m.streamingBuf.String()
	m.streamingBuf.Reset()
	m.isStreaming = false

	newLine := formatLine("assistant_stream", content)
	m.lines = append(m.lines, newLine)
	wrapped := truncateLines(wordwrap.String(newLine, m.termWidth), m.termWidth)
	return []tea.Cmd{tea.Printf("%s", wrapped)}
}

func (m *Model) View() string { //nolint:revive
	var b strings.Builder

	if m.showingCommands {
		m.renderCommandsPalette(&b)
		return b.String()
	}

	// If showing confirmation dialog, render it instead of normal input
	if m.showingConfirmation {
		m.renderConfirmationDialog(&b)
		return b.String()
	}

	// Render the input section.
	m.renderInputSection(&b)

	return b.String()
}

// renderInputSection renders the input and status section.
// It always outputs exactly 5 lines to keep the Bubble Tea renderer's
// linesRendered count stable; an unstable count causes CursorUp to
// over- or under-shoot and leaves ghost copies of the input block.
// Each line is also capped at m.termWidth columns so that wide characters
// or long help text never trigger terminal auto-wrapping, which would
// inflate the physical line count beyond what the renderer expects.
func (m *Model) renderInputSection(b *strings.Builder) {
	// Line 1: separator — cap at termWidth so it never auto-wraps on very
	// narrow terminals (termWidth < 5 would otherwise overflow the 5-rune
	// separator and break the physical-row invariant).
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	sep := "─────"
	sepWidth := xansi.StringWidth(sep)
	if m.termWidth > 0 && sepWidth > m.termWidth {
		sep = xansi.Truncate(sep, m.termWidth, "")
	}
	b.WriteString(separatorStyle.Render(sep) + "\n")

	// Line 2: status + permission mode tag (always rendered)
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorStatus)).
		Bold(true)
	status := m.status
	if status == "" {
		status = "就绪"
	}
	statusLine := statusStyle.Render("[状态] " + status)
	permTag := m.renderPermissionModeTag()
	var statusWithPerm string
	if permTag != "" {
		statusWithPerm = statusLine + "  " + permTag
	} else {
		statusWithPerm = statusLine
	}
	if m.termWidth > 0 && lipgloss.Width(statusWithPerm) > m.termWidth {
		// No tail: status/token lines are structured data; a hard cut is
		// cleaner than appending "…" which may misalign ANSI sequences.
		statusWithPerm = xansi.Truncate(statusWithPerm, m.termWidth, "")
	}
	b.WriteString(statusWithPerm + "\n")

	// Line 3: token status (always rendered)
	tokenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorStatus))
	tokenText := m.tokenStatus
	if tokenText == "" {
		tokenText = "---"
	}
	tokenLine := tokenStyle.Render("[令牌] " + tokenText)
	if m.termWidth > 0 && lipgloss.Width(tokenLine) > m.termWidth {
		// No tail: structured data line; hard cut avoids appending "…".
		tokenLine = xansi.Truncate(tokenLine, m.termWidth, "")
	}
	b.WriteString(tokenLine + "\n")

	// Line 4: input prompt
	inputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser))
	b.WriteString(inputStyle.Render("💬 ") + m.input.View() + "\n")

	// Line 5: help text (width-adaptive to prevent terminal auto-wrapping)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	b.WriteString(helpStyle.Render(m.buildHelpText()) + "\n")
}

// buildHelpText returns help text that fits within the terminal width.
// Progressively shorter versions are tried before falling back to a hard
// truncation. This prevents terminal auto-wrapping which would cause the
// renderer's line count to differ from actual displayed lines, leading to
// duplicate View output in non-AltScreen (inline) mode.
func (m *Model) buildHelpText() string {
	full := "Ctrl+C 退出 | Ctrl+Z 取消任务 | Ctrl+P 命令列表 | Tab 补全 | ↑↓ 历史"
	if m.termWidth <= 0 || lipgloss.Width(full) <= m.termWidth {
		return full
	}
	short := "Ctrl+C 退出 | Ctrl+Z 取消 | Ctrl+P 命令 | Tab 补全 | ↑↓ 历史"
	if lipgloss.Width(short) <= m.termWidth {
		return short
	}
	minimal := "^C退出 | ^Z取消 | ^P命令 | Tab补全"
	if lipgloss.Width(minimal) <= m.termWidth {
		return minimal
	}
	return xansi.Truncate(full, m.termWidth, "…")
}

// truncateLines ensures each line in the multi-line string does not exceed
// the given width in terminal columns. This prevents terminal auto-wrapping
// which would cause the renderer's line count to differ from actual displayed
// lines, leading to duplicate View output in non-AltScreen (inline) mode.
func truncateLines(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if xansi.StringWidth(line) > width {
			lines[i] = xansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

// Message defines the message structure
// -- Messages --
type Message struct {
	Role    string `json:"role"`
	Content string
}

// ThinkingMsg carries reasoning/thinking blocks with optional content.
type ThinkingMsg struct {
	Title          string
	Reasoning      string
	ReasoningDelta string
	Metadata       map[string]interface{}
}

type StatusUpdate string //nolint:revive

// ToolUseMsg carries structured tool-use information for deduplication.
type ToolUseMsg struct {
	ID       string
	ToolName string
	Status   string // "executing", "success", "error", "cancelled"
	Params   map[string]interface{}
	Result   string
}

// TokenStatsUpdate represents a token stats update
type TokenStatsUpdate struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Peak         float64
}

// ShowConfirmationMsg is sent via p.Send() to trigger the confirmation dialog
// from outside the Bubble Tea event loop (e.g., from the approval handler goroutine).
type ShowConfirmationMsg struct {
	Message  string
	ToolInfo map[string]interface{}
	Callback func(bool)
}

func formatLine(kind, s string) string {
	switch kind {
	case "assistant":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorAssistant)).Render("🤖 " + strings.TrimRight(s, "\n"))
	case "assistant_stream":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorAssistant)).Render(strings.TrimRight(s, "\n"))
	case "thinking":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorSystem)).Bold(true).Render("🧠 " + strings.TrimRight(s, "\n"))
	case "user":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser)).Bold(true).Render("👤 " + strings.TrimRight(s, "\n"))
	case "tool":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorTool)).Render("🛠️  " + strings.TrimRight(s, "\n"))
	case "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorError)).Bold(true).Render("❌ " + strings.TrimRight(s, "\n"))
	case "system":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorSystem)).Bold(true).Render("⚙️ " + strings.TrimRight(s, "\n"))
	default:
		return strings.TrimRight(s, "\n")
	}
}

func formatCount(num int) string {
	if num < 1000 {
		return fmt.Sprintf("%d", num)
	} else if num < 1000000 {
		return fmt.Sprintf("%.1fK", float64(num)/1000.0)
	} else { //nolint:revive
		return fmt.Sprintf("%.1fM", float64(num)/1000000.0)
	}
}

// truncateResult truncates a string to the specified maximum length (in runes)
func truncateResult(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// buildThinkingPreview builds a short preview of reasoning content for the status bar
func (m *Model) buildThinkingPreview() string {
	if m.thinkingReasoning == "" {
		return ""
	}
	runes := []rune(m.thinkingReasoning)
	if len(runes) > 80 {
		runes = runes[len(runes)-80:]
	}
	preview := string(runes)
	preview = strings.ReplaceAll(preview, "\n", " ")
	return strings.TrimSpace(preview)
}

// ShowConfirmation displays a confirmation dialog
func (m *Model) ShowConfirmation(message string, toolInfo map[string]interface{}, callback func(bool)) {
	m.showingConfirmation = true
	m.confirmationMessage = message
	m.confirmationToolInfo = toolInfo
	m.confirmationCallback = callback
	m.confirmationSelected = 0 // Default to "Yes"
}

// SetAllowlistHandler registers the callback invoked when the user selects
// "始终允许" (option 2) in the confirmation dialog.
func (m *Model) SetAllowlistHandler(h func(toolName string, params map[string]interface{})) {
	m.allowlistHandler = h
}

// SetPermissionManager wires a permission.Manager so that slash commands
// (/yolo, /permission, /allow, /disallow, /permissions) work in Bubble Tea mode.
func (m *Model) SetPermissionManager(mgr *permission.Manager) {
	m.permissionManager = mgr
	if mgr != nil {
		m.permissionMode = string(mgr.GetMode())
	}
}

// SetEngine wires an Engine so that slash commands (/think) can control
// thinking mode and other engine-level settings at runtime.
func (m *Model) SetEngine(eng *engine.Engine) {
	m.engine = eng
}

// SetAvailableToolNames sets the list of tool names used for Tab completion.
func (m *Model) SetAvailableToolNames(names []string) {
	m.availableToolNames = names
}

// handlePermissionSlashCommand intercepts locally-handled slash commands and
// returns (true, cmd) if the input was handled.
//
// Supported commands:
//
//	/yolo                – switch to YOLO mode
//	/permission <mode>   – set mode: default | acceptEdits | yolo
//	/allow <rule>        – add a session allowlist rule
//	/disallow <rule>     – remove a session allowlist rule
//	/permissions         – show current mode and allowlist rules
//	/think [on|off|status]  – control thinking mode (reasoning)
func (m *Model) handlePermissionSlashCommand(input string) (bool, tea.Cmd) {
	pm := m.permissionManager
	lower := strings.ToLower(strings.TrimSpace(input))

	switch {
	case lower == "/yolo":
		if pm == nil {
			line := m.renderPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		}
		pm.SetMode(permission.ModeYOLO)
		m.permissionMode = string(permission.ModeYOLO)
		line := m.renderPermissionFeedback("success",
			"⚡ YOLO 模式已激活",
			"所有工具将自动执行，无需确认。使用 /permission default 恢复。")
		m.lines = append(m.lines, line)
		return true, tea.Printf("%s", line)

	case lower == "/permissions":
		line := m.renderPermissionsPanel()
		m.lines = append(m.lines, line)
		return true, tea.Printf("%s", line)

	case strings.HasPrefix(lower, "/permission "):
		arg := strings.TrimSpace(input[len("/permission "):])
		if pm == nil {
			line := m.renderPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		}
		// Normalize arg to lowercase for case-insensitive matching
		mode := permission.PermissionMode(strings.ToLower(arg))
		// Map lowercase to canonical mode strings
		switch mode {
		case permission.PermissionMode(strings.ToLower(string(permission.ModeDefault))):
			mode = permission.ModeDefault
		case permission.PermissionMode(strings.ToLower(string(permission.ModeAcceptEdits))):
			mode = permission.ModeAcceptEdits
		case permission.PermissionMode(strings.ToLower(string(permission.ModeYOLO))):
			mode = permission.ModeYOLO
		}
		switch mode {
		case permission.ModeDefault, permission.ModeAcceptEdits, permission.ModeYOLO:
			pm.SetMode(mode)
			m.permissionMode = string(mode)
			line := m.renderPermissionFeedback("success",
				fmt.Sprintf("✅ 权限模式已切换为：%s", mode), "")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		default:
			line := m.renderPermissionFeedback("error",
				fmt.Sprintf("❌ 未知模式：%s", arg),
				"可选：default / acceptEdits / yolo")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		}

	case strings.HasPrefix(lower, "/allow "):
		raw := strings.TrimSpace(input[len("/allow "):])
		if raw == "" {
			line := m.renderPermissionFeedback("error",
				"❌ 规则不能为空",
				"示例：/allow Bash(git *) 或 /allow write_file(*.go)")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		}
		if pm == nil {
			line := m.renderPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		}
		rule := permission.ParseRule(raw)
		if rule.ToolName == "" {
			line := m.renderPermissionFeedback("error",
				fmt.Sprintf("❌ 无效规则：%q", raw), "")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		}
		pm.GetSessionAllowlist().AddRule(rule)
		line := m.renderPermissionFeedback("success",
			fmt.Sprintf("✅ 已添加白名单规则：%s", rule.RawPattern), "")
		m.lines = append(m.lines, line)
		return true, tea.Printf("%s", line)

	case strings.HasPrefix(lower, "/disallow "):
		raw := strings.TrimSpace(input[len("/disallow "):])
		if pm == nil {
			line := m.renderPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		}
		pm.GetSessionAllowlist().RemoveRule(raw)
		line := m.renderPermissionFeedback("success",
			fmt.Sprintf("🗑️ 已移除白名单规则：%s", raw), "")
		m.lines = append(m.lines, line)
		return true, tea.Printf("%s", line)

	case strings.HasPrefix(lower, "/think"):
		// Handle /think command via Engine
		if m.engine == nil {
			line := m.renderPermissionFeedback("error", "❌ Engine 未初始化", "")
			m.lines = append(m.lines, line)
			return true, tea.Printf("%s", line)
		}
		// Extract args after /think
		args := strings.TrimSpace(input[len("/think"):])
		result := m.engine.HandleThinkCommand(args)
		line := m.renderPermissionFeedback("info", result, "")
		m.lines = append(m.lines, line)
		return true, tea.Printf("%s", line)
	}

	return false, nil
}

// renderPermissionFeedback renders a colored feedback panel for permission commands.
func (m *Model) renderPermissionFeedback(level, title, detail string) string {
	var titleStyle, detailStyle lipgloss.Style
	switch level {
	case "success":
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSuccess))
		detailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorDetail))
	case "error":
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorError))
		detailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorDetail))
	default: // "info"
		titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorInfoTitle))
		detailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorDetail))
	}
	content := titleStyle.Render(title)
	if detail != "" {
		content += "\n" + detailStyle.Render(detail)
	}
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorMuted)).
		Padding(0, 1).
		Render(content)
}

// renderPermissionsPanel renders the full permission status panel.
func (m *Model) renderPermissionsPanel() string {
	pm := m.permissionManager
	if pm == nil {
		return m.renderPermissionFeedback("error", "❌ 权限管理器未初始化", "")
	}

	mode := pm.GetMode()
	rules := pm.GetSessionAllowlist().ListRules()

	modeIcon := m.permissionModeIcon(string(mode))
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s 当前权限模式：%s", modeIcon, mode)

	if len(rules) == 0 {
		sb.WriteString("\n\n📋 Session 白名单：（空）")
	} else {
		fmt.Fprintf(&sb, "\n\n📋 Session 白名单（%d 条规则）：", len(rules))
		for _, r := range rules {
			fmt.Fprintf(&sb, "\n  • %s", r.RawPattern)
		}
	}

	sb.WriteString("\n\n💡 可用命令：/yolo  /permission <mode>  /allow <rule>  /disallow <rule>")
	return m.renderPermissionFeedback("info", "🔐 权限状态", sb.String())
}

// renderPermissionModeTag renders a compact colored badge for the current
// permission mode, shown in the status bar only when a permission manager
// has been wired via SetPermissionManager.
func (m *Model) renderPermissionModeTag() string {
	// Only show the badge when a permission manager is actually wired.
	if m.permissionManager == nil {
		return ""
	}
	mode := m.permissionMode
	if mode == "" {
		mode = "default"
	}
	icon := m.permissionModeIcon(mode)
	var style lipgloss.Style
	switch permission.PermissionMode(mode) {
	case permission.ModeYOLO:
		style = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorOnAccent)).
			Background(lipgloss.Color(colorYoloBg))
	case permission.ModeAcceptEdits:
		style = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorOnAccent)).
			Background(lipgloss.Color(colorAcceptEditsBg))
	default:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorDefaultFg)).
			Background(lipgloss.Color(colorDimBg))
	}
	return style.Render(fmt.Sprintf(" %s %s ", icon, mode))
}

func (m *Model) permissionModeIcon(mode string) string {
	switch permission.PermissionMode(mode) {
	case permission.ModeYOLO:
		return "⚡"
	case permission.ModeAcceptEdits:
		return "✏️"
	default:
		return "🔒"
	}
}

// tabComplete performs Tab completion for slash commands.
// Returns (completed string, list of candidates).
func (m *Model) tabComplete(input string) (string, []string) {
	// Use cached names; populate on first use to avoid filesystem I/O on every
	// Tab press. The cache is refreshed each time the command palette is opened.
	if len(m.slashNames) == 0 {
		m.loadCommands()
	}
	allCmds := m.slashNames

	if input == "/" {
		return "", allCmds
	}

	// Complete command name (no space yet)
	if !strings.Contains(input, " ") {
		var matches []string
		for _, cmd := range allCmds {
			if strings.HasPrefix(cmd, input) {
				matches = append(matches, cmd)
			}
		}
		if len(matches) == 1 {
			// Commands that take no arguments don't need a trailing space
			noArgCmds := map[string]bool{"/yolo": true, "/permissions": true}
			suffix := " "
			if noArgCmds[matches[0]] {
				suffix = ""
			}
			return matches[0] + suffix, matches
		}
		return "", matches
	}

	lower := strings.ToLower(input)

	// /allow <tool> – complete tool name
	if strings.HasPrefix(lower, "/allow ") {
		partial := strings.TrimSpace(input[len("/allow "):])
		var matches []string
		for _, name := range m.availableToolNames {
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(partial)) {
				matches = append(matches, name)
			}
		}
		if len(matches) == 1 {
			return "/allow " + matches[0], matches
		}
		return "", matches
	}

	// /permission <mode> – complete mode name
	if strings.HasPrefix(lower, "/permission ") && !strings.HasPrefix(lower, "/permissions") {
		partial := strings.TrimSpace(input[len("/permission "):])
		modes := []string{"default", "acceptEdits", "yolo"}
		var matches []string
		for _, mode := range modes {
			if strings.HasPrefix(mode, partial) {
				matches = append(matches, mode)
			}
		}
		if len(matches) == 1 {
			return "/permission " + matches[0], matches
		}
		return "", matches
	}

	return "", nil
}

// hideConfirmation hides the confirmation dialog
func (m *Model) hideConfirmation() {
	m.showingConfirmation = false
	m.confirmationMessage = ""
	m.confirmationToolInfo = nil
	m.confirmationCallback = nil
	m.confirmationSelected = 0
}

// renderConfirmationDialog renders the confirmation dialog
func (m *Model) renderConfirmationDialog(b *strings.Builder) {
	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorWarning)).
		Background(lipgloss.Color(colorConfirmBg)).
		Padding(0, 1)
	b.WriteString(titleStyle.Render("⚠️  工具执行确认") + "\n\n")

	// Message
	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorBright)).
		Bold(true)
	b.WriteString(messageStyle.Render(m.confirmationMessage) + "\n\n")

	// Tool info if available
	if m.confirmationToolInfo != nil {
		infoStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorSecondary)).
			Italic(true)

		if toolName, ok := m.confirmationToolInfo["Name"].(string); ok {
			b.WriteString(infoStyle.Render("工具: "+toolName) + "\n")
		}

		// Display parameters instead of ID
		if params, ok := m.confirmationToolInfo["Parameters"]; ok {
			b.WriteString(infoStyle.Render("参数: ") + "\n")

			// Format parameters nicely
			if paramsMap, ok := params.(map[string]interface{}); ok {
				for key, value := range paramsMap {
					paramLine := fmt.Sprintf("  %s: %v", key, value)
					// Wrap long parameter values
					if len(paramLine) > 60 {
						paramLine = paramLine[:57] + "..."
					}
					b.WriteString(infoStyle.Render(paramLine) + "\n")
				}
			} else {
				// Fallback for non-map parameters
				paramStr := fmt.Sprintf("%v", params)
				if len(paramStr) > 60 {
					paramStr = paramStr[:57] + "..."
				}
				b.WriteString(infoStyle.Render("  "+paramStr) + "\n")
			}
		}
		b.WriteString("\n")
	}

	// Buttons
	yesStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorButtonFg)).
		Background(lipgloss.Color(colorYesButtonBg)).
		Padding(0, 2).
		Bold(true)

	noStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorButtonFg)).
		Background(lipgloss.Color(colorNoButtonBg)).
		Padding(0, 2).
		Bold(true)

	alwaysStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorButtonFg)).
		Background(lipgloss.Color(colorAlwaysButtonBg)).
		Padding(0, 2).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorSecondary)).
		Background(lipgloss.Color(colorDimBg)).
		Padding(0, 2)

	var yesButton, noButton, alwaysButton string
	switch m.confirmationSelected {
	case 0:
		yesButton = yesStyle.Render("✓ 确认")
		noButton = normalStyle.Render("✗ 取消")
		alwaysButton = normalStyle.Render("★ 始终允许")
	case 1:
		yesButton = normalStyle.Render("✓ 确认")
		noButton = noStyle.Render("✗ 取消")
		alwaysButton = normalStyle.Render("★ 始终允许")
	default:
		yesButton = normalStyle.Render("✓ 确认")
		noButton = normalStyle.Render("✗ 取消")
		alwaysButton = alwaysStyle.Render("★ 始终允许")
	}

	buttonsLine := lipgloss.JoinHorizontal(lipgloss.Left, yesButton, "  ", noButton, "  ", alwaysButton)
	b.WriteString(buttonsLine + "\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	b.WriteString(helpStyle.Render("← → 或 h l 选择，Enter 确认，Esc 取消") + "\n")
}

func (m *Model) openCommandsPalette() {
	m.loadCommands()
	m.showingCommands = true
	if m.commandsIndex >= len(m.commands) {
		m.commandsIndex = 0
	}
	m.commandsScrollOffset = 0 // reset scroll on every open
}

func (m *Model) loadCommands() {
	cwd := m.cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	reg := slash.NewRegistry(cwd)
	m.commands = reg.All()
	m.slashNames = reg.Names()
}

func (m *Model) renderCommandsPalette(b *strings.Builder) {
	const visibleRows = commandsPaletteVisibleRows

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorInfoTitle)).Render("命令列表")
	total := len(m.commands)
	if total > visibleRows {
		end := m.commandsScrollOffset + visibleRows
		if end > total {
			end = total
		}
		title += fmt.Sprintf(" (%d–%d / %d, ↑↓ 滚动)", m.commandsScrollOffset+1, end, total)
	}
	b.WriteString(title + "\n\n")

	// Group commands by category for display.
	categoryLabels := map[slash.Category]string{
		slash.CategoryPermission: "权限",
		slash.CategorySkill:      "Skills",
		slash.CategorySchedule:   "调度",
		slash.CategoryOpenSpec:   "OpenSpec",
		slash.CategoryCustom:     "自定义",
	}
	categoryColors := map[slash.Category]string{
		slash.CategoryPermission: colorWarning,
		slash.CategorySkill:      colorSuccess,
		slash.CategorySchedule:   colorStatus,
		slash.CategoryOpenSpec:   colorOpenSpec,
		slash.CategoryCustom:     colorSystem,
	}

	// Only render the visible slice.
	end := m.commandsScrollOffset + visibleRows
	if end > total {
		end = total
	}
	visible := m.commands[m.commandsScrollOffset:end]

	currentCat := slash.Category("")
	for idx, it := range visible {
		i := m.commandsScrollOffset + idx
		if it.Category != currentCat {
			currentCat = it.Category
			label := categoryLabels[currentCat]
			color := categoryColors[currentCat]
			catStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))
			b.WriteString("\n" + catStyle.Render("── "+label+" ──") + "\n")
		}
		prefix := "  "
		if i == m.commandsIndex {
			prefix = "> "
		}
		line := fmt.Sprintf("%s/%s  %s\n", prefix, it.Name, it.Description)
		if it.Category == slash.CategoryCustom && it.Source != "" {
			line = fmt.Sprintf("%s/%s  [%s] %s\n", prefix, it.Name, it.Source, it.Description)
		}
		b.WriteString(line)
	}
	b.WriteString("\n")

	if len(m.commands) > 0 {
		it := m.commands[m.commandsIndex]
		var pb strings.Builder
		fmt.Fprintf(&pb, "/%s\n", it.Name)
		if it.Usage != "" {
			fmt.Fprintf(&pb, "用法: %s\n", it.Usage)
		}
		if it.Namespace != "" {
			fmt.Fprintf(&pb, "命名空间: %s\n", it.Namespace)
		}
		if it.Description != "" {
			fmt.Fprintf(&pb, "描述: %s\n", it.Description)
		}
		if len(it.AllowedTools) > 0 {
			fmt.Fprintf(&pb, "允许工具: %s\n", strings.Join(it.AllowedTools, ", "))
		}
		if it.Category == slash.CategoryCustom {
			pb.WriteString("\n前置命令: 支持 ! 行，受 allowed-tools 中 run_shell_command 控制\n")
		}
		help := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("Enter 插入 | Esc 返回")
		box := lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(colorMuted)).Padding(1, 2).Render(pb.String())
		b.WriteString(box + "\n" + help + "\n")
	}
}
