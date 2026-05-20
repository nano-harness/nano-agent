package bubbletea

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/attachment"
	"github.com/nano-harness/nano-agent/pkg/config"
	"github.com/nano-harness/nano-agent/pkg/engine"
	"github.com/nano-harness/nano-agent/pkg/filesearch"
	"github.com/nano-harness/nano-agent/pkg/llm"
	"github.com/nano-harness/nano-agent/pkg/logger"
	"github.com/nano-harness/nano-agent/pkg/slash"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
	"github.com/nano-harness/nano-agent/pkg/ui/spinnerverbs"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/reflow/wordwrap"
)

// FullscreenModel is the fullscreen TUI model with virtual scrolling.
type FullscreenModel struct {
	// Message data
	messages *MessageStore

	// Virtual scrolling
	viewport *ViewportState

	// Input area
	textarea     textarea.Model
	inputHistory []string
	historyIndex int
	inputDraft   string

	// Terminal dimensions
	termWidth  int
	termHeight int

	// Shared state from inline model
	outbound func(eventsource.Outbound) error
	cwd      string
	apiURL   string

	// Display state
	ready  bool
	status string

	// Thinking block state. The collapsed/expanded state of a finished
	// thinking block now lives on the individual FormattedMessage
	// (msg.Collapsed) so multiple thinking blocks in a single
	// conversation can be toggled independently.
	thinkingTitle     string
	thinkingReasoning string
	thinkingCompleted bool
	thinkingPending   string
	thinkingWindow    []string

	// Dynamic status state
	currentPhase      displayPhase
	tokenStatus       string
	contextWindowMax  int
	contextUsedTokens int
	connectionState   string
	connectionDetail  string
	swarmLine         string
	cronIndicator     string
	activeToolCalls   map[string]string
	spinnerFrame      int
	spinnerStage      string
	spinnerVerbs      []string // Effective spinner verbs from config
	selectedVerb      string   // verb selected at start of thinking cycle

	// Confirmation dialog state
	showingConfirmation        bool
	confirmationMessage        string
	confirmationToolInfo       map[string]interface{}
	confirmationCallback       func(bool)
	confirmationAlwaysCallback func()
	confirmationSelected       int
	confirmationButtons        []hitBox

	// exiting is set when the user has requested a graceful exit so View()
	// can disable AltScreen on its next frame, allowing the dumped history
	// to remain in the terminal scrollback.
	exiting bool

	// dumpView is set when the [ shortcut temporarily drops the user back to
	// the main screen so they can search history with their terminal's
	// native scrollback. The next key press restores alt-screen.
	dumpView bool

	// Optional capabilities injected from the CLI layer. They are stored
	// so cmd/cli wiring can be symmetric with the inline model. The
	// fullscreen UI now renders the slash command palette (Ctrl+P) and
	// honours the permission manager for Shift+Tab cycling; the
	// confirmation dialog is also rendered. Other capabilities (e.g.
	// team scoping) are stored for future renderers.
	permissionManager *permission.Manager
	permissionMode    string
	modelLister       func() string
	newSessionHandler func() string

	// Extended slash-command capabilities. The fullscreen model accepts
	// the same set as the inline model so /models, /routines, /skills
	// etc. can be dispatched through the shared local dispatcher.
	skillLister          func() string
	modelStatusGetter    func() string
	modelSwitcher        func(string) string
	modelFallbackHandler func(string) string
	modelDoctor          func(string) string
	contextStatusGetter  func() string
	doctorReporter       func() string
	eventsQuerier        func(string) string
	auditQuerier         func(string) string
	routinesLister       func() string
	runningStatusLister  func() string
	routinesAdder        func(string) string
	routinesRemover      func(string) string
	routinesPauser       func(string) string
	routinesResumer      func(string) string
	routinesRunner       func(string) string
	allowlistHandler     func(toolName string, params map[string]interface{})
	teamName             string
	availableToolNames   []string

	// Persistent allowlist and engine (used by /disallow and /think).
	persistentAllowlist *permission.PersistentAllowlistStore
	workdir             string
	engine              *engine.Engine

	// Attachment manager for image paste and file input
	attachmentMgr  *attachment.Manager
	pendingImages  []llm.MultimodalImage
	imageIndicator string

	// bannerArt holds the rendered static product banner. When non-empty,
	// renderMessageArea displays it while no messages exist.
	bannerArt string

	// termCap describes which characters the host terminal can render.
	// Populated lazily by NewFullscreenModel and re-evaluated whenever
	// the environment changes (which in practice means: never during a
	// session, but we still recompute defensively on resize).
	termCap TermCapability

	// layout owns the responsive layout math: status bar / input / help
	// heights, content width and current LayoutMode. All renderers
	// consult it instead of computing dimensions ad hoc so the message
	// area, input panel and help line stay in lockstep.
	layout *LayoutEngine

	// Command palette state (Ctrl+P). Mirrors the inline palette so the
	// milktea mode offers the same browseable view of slash commands.
	showingCommands      bool
	commands             []slash.Command
	commandsIndex        int
	commandsScrollOffset int
	commandItems         []commandHitBox
	slashNames           []string

	// showHelp controls whether the full help line is visible. Toggled by
	// pressing `?`; starts collapsed so the input area gets maximum space.
	showHelp bool

	// File picker state — mirrors the inline model so `@filename` references
	// work in fullscreen mode. Populated lazily from the filesearch index.
	showingFilePicker bool
	filePickerQuery   string
	filePickerResults []string
	filePickerCursor  int
	fileIndex         *filesearch.Index

	// historySearch holds the active reverse-history search state. When
	// non-nil the `Ctrl+R` shortcut filters inputHistory by the search term.
	historySearch *HistorySearch

	// Streaming text aggregation — mirrors the inline model's throttling so
	// the fullscreen renderer is not forced to re-render on every chunk
	// received from high-throughput providers.
	streamingBuf    strings.Builder
	isStreaming     bool
	lastStreamFlush time.Time
	streamingMsgID  string // ID of the active streaming FormattedMessage

	// Local slash dispatcher mirrors the inline model so milktea handles
	// /reviewer-style agent profile commands and built-in helpers locally.
	localDispatcher *slash.LocalDispatcher
}

// NewFullscreenModel creates a new fullscreen model.
func NewFullscreenModel(cwd, apiURL string) *FullscreenModel {
	ta := textarea.New()
	ta.Placeholder = "输入消息... (Enter 发送, Shift+Enter 换行, Ctrl+C 退出)"
	ta.SetHeight(minInputHeight)
	ta.ShowLineNumbers = false
	// Newline insertion is handled explicitly so plain Enter can submit while
	// Shift+Enter/Ctrl+J still insert line breaks.
	ta.KeyMap.InsertNewline.SetEnabled(false)
	ta.CharLimit = 0
	applyTextareaTheme(&ta)
	ta.Focus()

	fileIndex, err := filesearch.NewIndex(cwd)
	if err == nil {
		if crawlErr := fileIndex.Crawl(); crawlErr != nil {
			logger.Warnf("FullscreenModel: failed to crawl file index: %v", crawlErr)
		}
	} else {
		logger.Warnf("FullscreenModel: failed to initialize file index: %v", err)
	}

	return &FullscreenModel{
		messages:   NewMessageStore(),
		viewport:   NewViewportState(10), // Initial height, will be updated
		textarea:   ta,
		cwd:        cwd,
		apiURL:     apiURL,
		termWidth:  80,
		termHeight: 24,
		ready:      false,
		status:     "等待输入",

		historyIndex:    -1,
		historySearch:   NewHistorySearch(nil),
		currentPhase:    phaseIdle,
		activeToolCalls: make(map[string]string),
		termCap:         DetectTermCapability(),
		layout:          NewLayoutEngine(),
		spinnerVerbs:    spinnerverbs.EffectiveVerbs(nil),
		fileIndex:       fileIndex,
	}
}

// Init initializes the model.
func (m *FullscreenModel) Init() tea.Cmd {
	// Ensure the textarea is focused so it receives every keystroke, including
	// characters like j/k/g/G that previously collided with vim-style scroll
	// shortcuts. Without this the input field silently swallowed those keys.
	m.textarea.Focus()
	return tea.Batch(textarea.Blink, spinnerTickCmd())
}

// SetBannerArt stores the rendered product banner artwork that the fullscreen
// View displays as default header content while the conversation is empty.
func (m *FullscreenModel) SetBannerArt(art string) {
	m.bannerArt = art
}

// SetSpinnerVerbsConfig updates the spinner verbs configuration.
func (m *FullscreenModel) SetSpinnerVerbsConfig(cfg *config.SpinnerVerbsConfig) {
	m.spinnerVerbs = spinnerverbs.EffectiveVerbs(cfg)
}

// Update handles messages and updates the model.
func (m *FullscreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.ready = true
		if m.layout == nil {
			m.layout = NewLayoutEngine()
		}
		m.layout.Update(msg.Width, msg.Height)

		// Compute the textarea inner width from the layout engine so all
		// renderers (input panel, help, message area) agree on the same
		// set of dimensions even on narrow terminals.
		m.textarea.SetWidth(m.layout.InputInnerWidth())
		m.updateLayoutHeights()

		// Re-render cached messages because their width depends on termWidth.
		m.messages.Range(func(_ int, fm *FormattedMessage) bool {
			m.renderMessage(fm)
			return true
		})
		m.updateViewportHeight()
		return m, nil

	case SpinnerTickMsg:
		// Suppress spinner ticks during dump-view or exit so the
		// inline renderer does not overwrite the Printf'd dump text.
		if m.dumpView || m.exiting {
			return m, nil
		}
		if m.isSpinnerPhase() {
			m.spinnerFrame++
		}
		// Throttled streaming flush: only re-render the active streaming
		// message when the flush interval has elapsed (mirrors inline model).
		if m.shouldFlushPartial() {
			m.flushStreamingIncremental()
		}
		return m, spinnerTickCmd()

	case tea.KeyPressMsg:
		// When in temporary dump view (Phase 4: `[` shortcut), any key
		// restores the alt-screen so the user can resume the session.
		if m.dumpView {
			m.dumpView = false
			return m, spinnerTickCmd()
		}
		if m.showingConfirmation {
			return m, m.handleConfirmationKey(msg)
		}

		// Command palette has priority over all other key handling so
		// arrow keys / Enter / Esc reach the palette before the textarea.
		if m.showingCommands {
			return m, m.handleCommandPaletteKey(msg)
		}

		// Reverse history search has second priority — key events are
		// consumed for search query editing until Enter / Esc exits.
		if m.historySearch != nil && m.historySearch.Active() {
			consumed := m.handleHistorySearchKey(msg)
			if consumed {
				return m, nil
			}
		}

		// Submission and exit shortcuts. All other keys (including j/k/g/G,
		// arrow keys, and printable characters) fall through to the textarea so
		// the input field can never swallow user typing.
		switch msg.String() {
		case "ctrl+c":
			m.exiting = true
			dump := m.DumpHistoryToScrollback()
			if dump == "" {
				return m, tea.Quit
			}
			return m, tea.Sequence(tea.Printf("%s", dump), tea.Quit)

		case "[":
			if strings.TrimSpace(m.textarea.Value()) == "" {
				m.dumpView = true
				dump := m.DumpHistoryToScrollback()
				if dump == "" {
					return m, nil
				}
				return m, tea.Printf("%s", dump)
			}

		case "enter":
			if msg.Code == tea.KeyEnter && msg.Mod&tea.ModShift != 0 {
				m.textarea.InsertString("\n")
				return m, nil
			}
			if isContinuationInput(m.textarea.Value()) {
				m.textarea.SetValue(strings.TrimSuffix(m.textarea.Value(), "\\") + "\n")
				m.textarea.CursorEnd()
				return m, nil
			}
			m.submitInput()
			return m, nil

		case "ctrl+j", "shift+enter":
			m.textarea.InsertString("\n")
			return m, nil

		case "ctrl+d":
			m.submitInput()
			return m, nil

		case "ctrl+z":
			return m, m.outboundCmd(eventsource.Outbound{Kind: "cancel"})

		case "ctrl+t":
			if last := m.lastThinkingMessage(); last != nil {
				last.Toggle()
				m.renderMessage(last)
				m.updateViewportHeight()
			}
			return m, nil

		case "ctrl+o":
			// Toggle the collapsed/expanded state of the most recent
			// tool message so users can inspect full results on demand
			// without permanently widening the message stream.
			if last := m.lastToolMessage(); last != nil {
				last.Toggle()
				last.InvalidateCache()
				m.renderMessage(last)
				m.updateViewportHeight()
			}
			return m, nil

		case "ctrl+y":
			if last := m.lastAssistantReply(); last != "" {
				return m, tea.SetClipboard(last)
			}
			return m, nil

		case "ctrl+l":
			return m, m.clearSessionCmd()

		case "ctrl+p":
			m.openCommandsPalette()
			return m, nil

		case "ctrl+r":
			// Ctrl+R: start/advance reverse history search (same as inline model).
			m.startHistorySearch()
			return m, nil

		case "ctrl+f":
			m.dumpView = true
			dump := m.DumpHistoryToScrollback()
			if dump == "" {
				return m, nil
			}
			return m, tea.Printf("%s", dump)

		case "shift+tab":
			if m.permissionManager != nil {
				next := cyclePermissionMode(m.permissionMode)
				m.permissionManager.SetMode(next)
				m.permissionMode = string(next)
				m.recordMessage("system", fmt.Sprintf("权限模式 → %s %s", m.permissionModeIcon(string(next)), next))
			}
			return m, nil

		case "?", "shift+/":
			if strings.TrimSpace(m.textarea.Value()) == "" {
				m.showHelp = !m.showHelp
				m.updateLayoutHeights()
				return m, nil
			}

		case "tab":
			if m.showingFilePicker && len(m.filePickerResults) > 0 {
				m.insertSelectedFile()
				m.showingFilePicker = false
				return m, nil
			}
			m.handleTabCompletion()
			return m, nil

		case "up":
			if m.showingFilePicker {
				if m.filePickerCursor > 0 {
					m.filePickerCursor--
				}
				return m, nil
			}
			if m.textarea.Line() > 0 || len(m.inputHistory) == 0 {
				break
			}
			if m.historyIndex == -1 {
				m.inputDraft = m.textarea.Value()
				m.historyIndex = len(m.inputHistory) - 1
			} else if m.historyIndex > 0 {
				m.historyIndex--
			}
			m.textarea.SetValue(m.inputHistory[m.historyIndex])
			m.textarea.CursorEnd()
			return m, nil

		case "down":
			if m.showingFilePicker {
				if m.filePickerCursor < len(m.filePickerResults)-1 {
					m.filePickerCursor++
				}
				return m, nil
			}
			if m.textarea.Line() < m.textarea.LineCount()-1 || m.historyIndex == -1 {
				break
			}
			if m.historyIndex < len(m.inputHistory)-1 {
				m.historyIndex++
				m.textarea.SetValue(m.inputHistory[m.historyIndex])
			} else {
				m.historyIndex = -1
				m.textarea.SetValue(m.inputDraft)
			}
			m.textarea.CursorEnd()
			return m, nil

		case "esc":
			if m.showingFilePicker {
				m.showingFilePicker = false
				m.filePickerResults = nil
				return m, nil
			}
			if m.historyIndex != -1 {
				m.historyIndex = -1
				m.textarea.SetValue(m.inputDraft)
				m.textarea.CursorEnd()
				m.status = m.formatStatusForPhase(m.currentPhase, "")
				return m, nil
			}

		case "pgup", "ctrl+u":
			m.viewport.PageUp()
			return m, nil

		case "pgdown":
			m.viewport.PageDown()
			return m, nil

		case "home":
			m.viewport.ScrollToTop()
			return m, nil

		case "end":
			m.viewport.ScrollToBottom()
			return m, nil
		}

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft && m.showingConfirmation {
			if btn := m.hitTestConfirmationButton(mouse.X, mouse.Y); btn >= 0 {
				m.confirmationSelected = btn
				cmd := m.buildConfirmationCmd()
				m.hideConfirmation()
				return m, cmd
			}
			return m, nil
		}
		if mouse.Button == tea.MouseLeft && m.showingCommands {
			if idx := m.hitTestCommandItem(mouse.X, mouse.Y); idx >= 0 {
				m.commandsIndex = idx
				m.insertSelectedCommand()
				m.showingCommands = false
			}
			return m, nil
		}

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if m.showingCommands {
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.moveCommandSelection(-1)
			case tea.MouseWheelDown:
				m.moveCommandSelection(1)
			}
			return m, nil
		}
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.viewport.ScrollUp(historyWheelScrollLines)
		case tea.MouseWheelDown:
			m.viewport.ScrollDown(historyWheelScrollLines)
		}
		return m, nil

	case Message:
		// assistant_stream chunks are buffered; non-streaming messages flush
		// any pending buffer before being appended so ordering is preserved.
		if msg.Role == "assistant_stream" {
			m.streamingBuf.WriteString(msg.Content)
			m.isStreaming = true
			if m.shouldFlushPartial() {
				m.flushStreamingIncremental()
			}
		} else {
			m.flushStreamingBuffer()
			m.handleIncomingMessage(msg)
		}
		return m, nil

	case ThinkingMsg:
		m.flushStreamingBuffer()
		m.handleThinkingMessage(msg)
		m.updateLayoutHeights()
		return m, nil

	case ToolUseMsg:
		m.flushStreamingBuffer()
		m.handleToolUseMessage(msg)
		m.updateLayoutHeights()
		return m, nil

	case StatusUpdate:
		m.flushStreamingBuffer()
		m.handleStatusUpdate(msg)
		m.updateLayoutHeights()
		return m, nil

	case TaskCompletionMsg:
		m.flushStreamingBuffer()
		m.resetThinkingState()
		m.activeToolCalls = make(map[string]string)
		m.resetSpinnerToIdle()
		m.currentPhase = phaseDone
		m.status = m.formatStatusForPhase(phaseDone, "")
		m.updateLayoutHeights()
		return m, nil

	case CronStatusMsg:
		m.cronIndicator = msg.Indicator
		m.updateLayoutHeights()
		return m, nil

	case TokenStatsUpdate:
		m.handleTokenStatsUpdate(msg)
		m.updateLayoutHeights()
		return m, nil

	case ConnectionStatusMsg:
		m.connectionState = msg.State
		m.connectionDetail = msg.Detail
		m.updateLayoutHeights()
		return m, nil

	case NoticeMsg:
		m.recordMessage("system", string(msg))
		return m, nil

	case MailboxMsg:
		m.swarmLine = fmt.Sprintf("📬 %s → %s [%s] %s", msg.From, msg.To, msg.Kind, msg.Preview)
		m.updateLayoutHeights()
		return m, nil

	case IdleNotifyMsg:
		m.swarmLine = fmt.Sprintf("💤 %s: %s", msg.Agent, msg.Summary)
		m.updateLayoutHeights()
		return m, nil

	case SpawnTeammateMsg:
		m.swarmLine = fmt.Sprintf("🧑‍💻 %s spawned for %s (%s)", msg.Agent, msg.Topic, msg.SessionID)
		m.updateLayoutHeights()
		return m, nil

	case ShowConfirmationMsg:
		m.showingConfirmation = true
		m.confirmationMessage = msg.Message
		m.confirmationToolInfo = msg.ToolInfo
		m.confirmationCallback = msg.Callback
		m.confirmationAlwaysCallback = msg.AlwaysCallback
		m.confirmationSelected = 0
		toolName, _ := msg.ToolInfo["Name"].(string)
		m.setAgentStatus(phaseAwaitingApproval, toolName)
		return m, nil
	}

	// Update textarea — every key event that wasn't explicitly handled above
	// (including printable characters and vim-style letters) reaches here.
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	// Update file picker state whenever the textarea content changes so
	// `@filename` completions stay in sync with the current input value.
	m.updateFilePickerState()

	return m, tea.Batch(cmds...)
}

// submitInput sends the current textarea contents as a user message.
func (m *FullscreenModel) submitInput() {
	input := strings.TrimSpace(m.textarea.Value())
	if input == "" {
		return
	}

	// Expand @filename references before routing so slash commands and the
	// backend both receive the resolved file content instead of the raw token.
	if strings.Contains(input, "@") {
		input = ExpandFileReferences(input, m.cwd)
	}

	// Route slash commands through the shared local dispatcher first so
	// /reviewer agent profile commands are rewritten and built-in helpers
	// render system messages instead of being forwarded as plain text.
	if strings.HasPrefix(input, "/") {
		dispatched := m.dispatchLocalSlash(input)
		if dispatched.Handled {
			role := "system"
			if dispatched.Level == "error" {
				role = "error"
			}
			m.addSimpleMessage(role, dispatched.Message)
			m.recordSubmittedHistory(input)
			m.textarea.Reset()
			return
		}
		if dispatched.ShouldSubmit {
			m.addUserMessage(input)
			m.recordSubmittedHistory(input)
			m.currentPhase = phaseProcessing
			m.status = m.formatStatusForPhase(phaseProcessing, "")
			m.textarea.Reset()
			if m.outbound != nil {
				_ = m.outbound(eventsource.Outbound{
					Kind: "user_message",
					Text: dispatched.SubmitInput,
				})
			}
			return
		}
	}

	m.addUserMessage(input)
	m.recordSubmittedHistory(input)
	m.currentPhase = phaseProcessing
	m.status = m.formatStatusForPhase(phaseProcessing, "")
	m.textarea.Reset()
	if m.outbound != nil {
		_ = m.outbound(eventsource.Outbound{
			Kind: "user_message",
			Text: input,
		})
	}
}

// recordSubmittedHistory appends input to the textarea history (dedup'd) and
// resets the navigation cursor.
func (m *FullscreenModel) recordSubmittedHistory(input string) {
	if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != input {
		m.inputHistory = append(m.inputHistory, input)
		if len(m.inputHistory) > 100 {
			m.inputHistory = m.inputHistory[1:]
		}
	}
	m.historyIndex = -1
	m.inputDraft = ""
}

// addSimpleMessage appends a non-streaming message (typically a system
// notice) to the message list.
func (m *FullscreenModel) addSimpleMessage(role, content string) {
	msg := NewFormattedMessage(fmt.Sprintf("%s-%d", role, time.Now().UnixNano()), role, content)
	m.renderMessage(msg)
	m.messages.AddMessage(msg)
	m.updateViewportHeight()
}

// detectFileMentionContext returns (true, query) when the textarea's current
// input ends with a `@query` that is eligible for file completion. Returns
// (false, "") otherwise — mirrors the inline model's logic.
func (m *FullscreenModel) detectFileMentionContext() (bool, string) {
	text := m.textarea.Value()
	if text == "" {
		return false, ""
	}
	i := len(text) - 1
	for i >= 0 {
		r := rune(text[i])
		if r == '@' {
			if i == 0 || text[i-1] == ' ' || text[i-1] == '\n' || text[i-1] == '\t' {
				return true, text[i+1:]
			}
			return false, ""
		}
		if r == ' ' || r == '\n' || r == '\t' {
			return false, ""
		}
		i--
	}
	return false, ""
}

// updateFilePickerState refreshes the file-picker results based on the current
// textarea value. Called after every textarea update.
func (m *FullscreenModel) updateFilePickerState() {
	active, query := m.detectFileMentionContext()
	if !active {
		m.showingFilePicker = false
		m.filePickerQuery = ""
		m.filePickerResults = nil
		m.filePickerCursor = 0
		return
	}
	m.showingFilePicker = true
	m.filePickerQuery = query
	if m.fileIndex == nil {
		return
	}
	m.filePickerResults = m.fileIndex.Search(query, maxFilePickerResults)
	if m.filePickerCursor >= len(m.filePickerResults) {
		m.filePickerCursor = len(m.filePickerResults) - 1
	}
	if m.filePickerCursor < 0 {
		m.filePickerCursor = 0
	}
}

// insertSelectedFile replaces the `@query` token with the selected file path.
func (m *FullscreenModel) insertSelectedFile() {
	if len(m.filePickerResults) == 0 || m.filePickerCursor < 0 || m.filePickerCursor >= len(m.filePickerResults) {
		return
	}
	text := m.textarea.Value()
	idx := strings.LastIndex(text, "@")
	if idx < 0 {
		return
	}
	replacement := "@" + m.filePickerResults[m.filePickerCursor] + " "
	m.textarea.SetValue(text[:idx] + replacement)
	m.textarea.CursorEnd()
}

// startHistorySearch initiates or advances reverse history search — mirrors
// the inline model's logic but drives the FullscreenModel's textarea.
func (m *FullscreenModel) startHistorySearch() {
	if m.historySearch == nil {
		m.historySearch = NewHistorySearch(m.inputHistory)
	}
	if !m.historySearch.Active() {
		m.inputDraft = m.textarea.Value()
		m.historySearch = NewHistorySearch(m.inputHistory)
		m.historySearch.Begin()
		m.status = "历史搜索：输入关键词，Ctrl+R 查找更早记录，Enter 接受，Esc 取消"
		return
	}
	m.historySearch.Next()
	m.applyHistorySearchSelection()
}

// handleHistorySearchKey routes key events when historySearch is active.
// Returns true when the key was consumed; false to fall through to normal
// handling.
func (m *FullscreenModel) handleHistorySearchKey(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "ctrl+r":
		m.historySearch.Next()
		m.applyHistorySearchSelection()
		return true
	case "enter":
		m.historySearch.End()
		m.historyIndex = -1
		m.status = m.formatStatusForPhase(m.currentPhase, "")
		return true
	case "esc", "ctrl+c":
		m.textarea.SetValue(m.inputDraft)
		m.textarea.CursorEnd()
		m.historySearch.End()
		m.historyIndex = -1
		m.status = m.formatStatusForPhase(m.currentPhase, "")
		return true
	case "backspace":
		m.historySearch.Backspace()
		m.applyHistorySearchSelection()
		return true
	}
	if msg.Text != "" {
		for _, r := range msg.Text {
			m.historySearch.TypeRune(r)
		}
		m.applyHistorySearchSelection()
		return true
	}
	return false
}

// applyHistorySearchSelection updates the textarea content and status bar to
// reflect the current history search state.
func (m *FullscreenModel) applyHistorySearchSelection() {
	query := m.historySearch.Query()
	selected := m.historySearch.Selected()
	if selected != "" {
		m.textarea.SetValue(selected)
		m.textarea.CursorEnd()
		m.status = fmt.Sprintf("历史搜索：%q → %s", query, truncateHistoryPreview(selected))
		return
	}
	if query == "" {
		m.textarea.SetValue(m.inputDraft)
		m.textarea.CursorEnd()
		m.status = "历史搜索：输入关键词，Ctrl+R 查找更早记录，Enter 接受，Esc 取消"
		return
	}
	m.status = fmt.Sprintf("历史搜索：%q 无匹配", query)
}

// renderFilePicker renders the file-completion popup. Returns "" when the
// picker is not active or there are no results.
func (m *FullscreenModel) renderFilePicker() string {
	if !m.showingFilePicker {
		return ""
	}
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorHelpText)).
		Padding(0, 1)
	lines := []string{fmt.Sprintf("# 文件: %s", m.filePickerQuery)}
	if len(m.filePickerResults) == 0 {
		lines = append(lines, "  (no matches)")
	} else {
		for i, f := range m.filePickerResults {
			prefix := "  "
			if i == m.filePickerCursor {
				prefix = "▶ "
			}
			lines = append(lines, prefix+f)
		}
		lines = append(lines, "  ↑↓ 选择  Tab 确认  Esc 取消")
	}
	return style.Render(strings.Join(lines, "\n"))
}

// dispatchLocalSlash lazily constructs and invokes the local slash dispatcher.
// The dispatcher is built with the same capabilities as the inline model so
// /models, /routines, /skills and friends all work in fullscreen mode.
func (m *FullscreenModel) dispatchLocalSlash(input string) slash.Result {
	if m.localDispatcher == nil {
		m.localDispatcher = m.buildLocalDispatcher()
	}
	return m.localDispatcher.Dispatch(input)
}

// buildLocalDispatcher constructs the local dispatcher wired with all
// capabilities that have been injected by the CLI layer.
func (m *FullscreenModel) buildLocalDispatcher() *slash.LocalDispatcher {
	d := slash.NewLocalDispatcher(m.teamName, m.cwd).
		WithCheckpointer(slash.NewDefaultCheckpointManager(m.cwd))
	if m.modelLister != nil {
		d = d.WithModelLister(m.modelLister)
	}
	if m.skillLister != nil {
		d = d.WithSkillLister(m.skillLister)
	}
	if m.modelStatusGetter != nil {
		d = d.WithModelStatusGetter(m.modelStatusGetter)
	}
	if m.modelSwitcher != nil {
		d = d.WithModelSwitcher(m.modelSwitcher)
	}
	if m.modelFallbackHandler != nil {
		d = d.WithModelFallbackHandler(m.modelFallbackHandler)
	}
	if m.modelDoctor != nil {
		d = d.WithModelDoctor(m.modelDoctor)
	}
	if m.contextStatusGetter != nil {
		d = d.WithContextStatusGetter(m.contextStatusGetter)
	}
	if m.doctorReporter != nil {
		d = d.WithDoctorReporter(m.doctorReporter)
	}
	if m.eventsQuerier != nil {
		d = d.WithEventsQuerier(m.eventsQuerier)
	}
	if m.auditQuerier != nil {
		d = d.WithAuditQuerier(m.auditQuerier)
	}
	if m.routinesLister != nil {
		d = d.WithRoutinesLister(m.routinesLister)
	}
	if m.runningStatusLister != nil {
		d = d.WithRunningStatusLister(m.runningStatusLister)
	}
	if m.routinesAdder != nil {
		d = d.WithRoutinesAdder(m.routinesAdder)
	}
	if m.routinesRemover != nil {
		d = d.WithRoutinesRemover(m.routinesRemover)
	}
	if m.routinesPauser != nil {
		d = d.WithRoutinesPauser(m.routinesPauser)
	}
	if m.routinesResumer != nil {
		d = d.WithRoutinesResumer(m.routinesResumer)
	}
	if m.routinesRunner != nil {
		d = d.WithRoutinesRunner(m.routinesRunner)
	}
	return d
}

// View renders the fullscreen TUI.
func (m *FullscreenModel) View() tea.View {
	if !m.ready {
		return tea.NewView("Initializing...")
	}

	// When exiting or in dump-view mode, drop back to the main screen so the
	// previously dumped history (emitted via tea.Printf) stays visible in
	// the terminal's native scrollback.
	if m.exiting || m.dumpView {
		v := tea.NewView("")
		v.AltScreen = false
		return v
	}
	if m.showingConfirmation {
		var b strings.Builder
		m.renderConfirmationDialog(&b)
		v := tea.NewView(b.String())
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		v.KeyboardEnhancements.ReportEventTypes = true
		return v
	}
	if m.showingCommands {
		var b strings.Builder
		// Render the status bar so the palette is framed by the same
		// chrome users see during normal interaction.
		b.WriteString(m.renderStatusBar())
		b.WriteString("\n")
		m.renderCommandsPalette(&b)
		v := tea.NewView(b.String())
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		v.KeyboardEnhancements.ReportEventTypes = true
		return v
	}

	var b strings.Builder

	// 1. Status bar (content line + separator rule).
	b.WriteString(m.renderStatusBar())
	b.WriteString("\n")

	// 2. Message list area (virtual scrolling). This area fills the remaining
	//    space above the floating input panel.
	b.WriteString(m.renderMessageArea())

	// 3. Floating input panel + help hint at the bottom.
	b.WriteString(m.renderInputArea())

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	// Enable disambiguated key events so Shift+Enter (and similar modifier
	// combinations) can be distinguished from plain Enter on capable
	// terminals.
	v.KeyboardEnhancements.ReportEventTypes = true
	return v
}

// renderStatusBar renders the top status bar as a content line containing
// the agent identity, current phase, a compact context indicator and a
// scroll percentage. Token statistics, the thinking window and the
// swarm line that the previous design stacked into the bar are now
// surfaced inside the message stream so the bar's height stays fixed
// at exactly two rows: content plus a separator rule (see
// LayoutEngine.StatusBarHeight).
func (m *FullscreenModel) renderStatusBar() string {
	width := m.termWidth
	if width <= 0 {
		width = 80
	}

	status := m.status
	if status == "" {
		status = "等待输入"
	}
	if m.cronIndicator != "" {
		status = m.cronIndicator + " | " + status
	}
	if spinner := m.currentSpinnerFrame(); spinner != "" {
		verb := m.currentSpinnerVerb()
		if verb != "" {
			status = spinner + " " + verb + " " + status
		} else {
			status = spinner + " " + status
		}
	}

	left := fmt.Sprintf(" %s", status)
	right := m.buildCompactStats()
	if m.layout != nil && m.layout.Mode() == LayoutMinimal {
		// Surface a compact marker so users on very narrow terminals
		// know the UI is in minimal-layout fallback mode. Place it on
		// the right side so the status bar truncation (which truncates
		// the left side first) never drops it.
		if right == "" {
			right = "[窄]"
		} else {
			right = "[窄] " + right
		}
	}
	if permTag := m.renderPermissionModeTag(); permTag != "" {
		if right == "" {
			right = permTag
		} else {
			right = permTag + "  " + right
		}
	}

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	usable := width
	if usable < 1 {
		usable = 1
	}
	gap := usable - leftWidth - rightWidth
	if gap < 1 {
		// Truncate the left side to keep the right-aligned stats visible.
		budget := usable - rightWidth - 1
		if budget < 4 {
			budget = 4
		}
		left = truncatePlain(left, budget)
		leftWidth = lipgloss.Width(left)
		gap = usable - leftWidth - rightWidth
		if gap < 1 {
			gap = 1
		}
	}

	if right != "" {
		right = fsStatusBarDimStyle().Render(right)
	}
	line := left + strings.Repeat(" ", gap) + right
	line = truncatePlain(line, usable)
	return fsStatusBarStyle(width).Render(line) + "\n" + fsStatusBarRule(width, m.termCap)
}

// buildCompactStats returns a single-line, space-separated stats line
// that fits next to the right edge of the status bar. It includes the
// context-window percentage (with a SafeChar progress bar), the input/
// output token counts, the connection state, and brief cwd/apiURL badges
// when set.
func (m *FullscreenModel) buildCompactStats() string {
	var parts []string
	if m.contextWindowMax > 0 {
		pct := float64(m.contextUsedTokens) / float64(m.contextWindowMax)
		if pct < 0 {
			pct = 0
		}
		if pct > 1 {
			pct = 1
		}
		barWidth := 10
		if m.layout != nil && m.layout.Mode() >= LayoutNarrow {
			barWidth = 6
		}
		filled := int(pct * float64(barWidth))
		bar := SafeProgressBar(filled, barWidth, m.termCap)
		parts = append(parts, fmt.Sprintf("[%s] %.0f%%", bar, pct*100))
		parts = append(parts, fmt.Sprintf("%s/%s",
			formatCount(m.contextUsedTokens), formatCount(m.contextWindowMax)))
	} else if m.contextUsedTokens > 0 {
		parts = append(parts, formatCount(m.contextUsedTokens))
	}
	if m.connectionState != "" && m.layout != nil && m.layout.Mode() <= LayoutNormal {
		parts = append(parts, m.connectionState)
	}
	// Show cwd basename and API URL badges on normal-width layouts only to
	// avoid crowding narrow/minimal layouts.
	if m.layout == nil || m.layout.Mode() <= LayoutNormal {
		if m.cwd != "" {
			base := m.cwd
			// Show only the last two path components to keep the badge compact.
			if idx := strings.LastIndex(base, "/"); idx >= 0 && idx < len(base)-1 {
				parent := base[:idx]
				if pidx := strings.LastIndex(parent, "/"); pidx >= 0 {
					base = base[pidx+1:]
				} else {
					base = base[idx+1:]
				}
			}
			parts = append(parts, SafeChar("folder_prefix", m.termCap)+base)
		}
		if m.apiURL != "" {
			parts = append(parts, SafeChar("globe_prefix", m.termCap)+m.apiURL)
		}
	}
	return strings.Join(parts, "  ")
}

// renderMessageArea renders the virtual scrolling message area.
func (m *FullscreenModel) renderMessageArea() string {
	if m.messages.Len() == 0 {
		return m.renderWelcomePage()
	}

	// Build height array
	heights := make([]int, m.messages.Len())
	m.messages.Range(func(i int, msg *FormattedMessage) bool {
		if msg.Rendered == "" {
			m.renderMessage(msg)
		}
		heights[i] = msg.Height
		return true
	})

	// Calculate visible range. With overscan the range can extend above
	// and below the literal viewport — we still need to anchor the
	// rendered output at the correct scroll position, which we achieve
	// with a single leading-skip rather than per-line padding.
	//
	// `topSpacer` is the cumulative height of all messages preceding
	// `startIdx`; subtracting it from ScrollOffset gives `leadingSkip`,
	// i.e. the number of rendered lines we need to drop from the top
	// of the mounted block so the first on-screen line matches the
	// caller's scroll position.
	startIdx, endIdx := m.viewport.VisibleRange(heights)
	topSpacer := m.viewport.TopSpacerHeight(heights, startIdx)
	leadingSkip := m.viewport.ScrollOffset - topSpacer
	if leadingSkip < 0 {
		leadingSkip = 0
	}

	var b strings.Builder
	for i := startIdx; i < endIdx; i++ {
		msg := m.messages.Get(i)
		b.WriteString(msg.Rendered)
		b.WriteString("\n")
	}

	rendered := b.String()
	if leadingSkip > 0 {
		// Drop `leadingSkip` lines from the top of the rendered block so
		// the visible window starts at ScrollOffset. strings.SplitN with
		// N = leadingSkip+1 yields at most N parts where the final part
		// is the unsplit remainder; picking lines[leadingSkip] therefore
		// returns everything after the first `leadingSkip` newlines.
		lines := strings.SplitN(rendered, "\n", leadingSkip+1)
		if len(lines) > leadingSkip {
			rendered = lines[leadingSkip]
		} else {
			rendered = ""
		}
	}

	return padToHeight(rendered, m.viewport.ViewportHeight)
}

// renderWelcomePage produces a centered welcome page shown when the
// conversation is empty. It replaces the previous animated banner
// playback (which appeared briefly on the main screen before the
// alt-screen took over — the "banner flash" issue). Now the banner is
// rendered statically inside the alt-screen so users see a stable
// landing page until they send their first message.
func (m *FullscreenModel) renderWelcomePage() string {
	contentWidth := m.termWidth
	if contentWidth <= 0 {
		contentWidth = 80
	}
	height := m.viewport.ViewportHeight
	if height <= 0 {
		return ""
	}

	hintText := "输入你的第一个问题，开始一段对话"
	tipText := "提示：/help 查看命令 · Ctrl+T 切换思考面板 · Ctrl+L 新会话"
	if m.layout != nil && m.layout.Mode() >= LayoutNarrow {
		hintText = "在下方输入框输入消息"
		tipText = "/help · ^T 思考 · ^L 新会话"
	}

	hint := fsWelcomeHintStyle().Width(contentWidth).Render(hintText)
	tip := fsWelcomeTipStyle().Width(contentWidth).Render(tipText)

	// Choose banner size based on available viewport height.
	var banner string
	switch {
	case height >= 30 && m.bannerArt != "":
		banner = m.bannerArt
	case height >= 15:
		banner = defaultMilkTeaAscii
	default:
		banner = "nano-agent"
	}
	// Center the banner horizontally inside the available width by
	// padding each line. We avoid lipgloss.PlaceHorizontal here so
	// embedded ANSI styling is preserved verbatim.
	banner = centerBlock(banner, contentWidth)

	body := banner + "\n\n" + hint + "\n" + tip
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) >= height {
		// Truncate from the top of the banner so the hint stays visible.
		bodyLines = bodyLines[len(bodyLines)-height:]
		return strings.Join(bodyLines, "\n")
	}
	// Vertically center: half the slack goes above the banner.
	slack := height - len(bodyLines)
	top := slack / 2
	return strings.Repeat("\n", top) + body + strings.Repeat("\n", slack-top)
}

// defaultMilkTeaAscii is the fallback art used when no banner was
// supplied via SetBannerArt. It is intentionally compact so it fits
// even on minimal terminals.
const defaultMilkTeaAscii = "" +
	"    .=|=.\n" +
	"    |o.o|     nano-agent\n" +
	"     \\_/"

// centerBlock pads each line of s with leading spaces so the longest
// line is centered inside `width` columns. Embedded ANSI escape
// sequences are preserved.
func centerBlock(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	maxw := 0
	for _, l := range lines {
		w := lipgloss.Width(l)
		if w > maxw {
			maxw = w
		}
	}
	if maxw >= width {
		return s
	}
	pad := strings.Repeat(" ", (width-maxw)/2)
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

// padToHeight pads the given string with blank lines so that it occupies
// exactly `height` rows. Used to anchor the floating input panel at the
// bottom of the screen.
func padToHeight(s string, height int) string {
	if height <= 0 {
		return s
	}
	lines := strings.Count(s, "\n")
	// `s` may or may not end with a newline. Trailing newline already counts
	// toward `lines`.
	if !strings.HasSuffix(s, "\n") && s != "" {
		lines++
	}
	if lines >= height {
		return s
	}
	pad := strings.Repeat("\n", height-lines)
	if !strings.HasSuffix(s, "\n") && s != "" {
		return s + "\n" + pad[1:]
	}
	return s + pad
}

// renderInputArea renders the floating input panel and help hint. On
// minimal terminals the rounded border is dropped to give the
// conversation as many rows as possible; the textarea content still
// fills the full width so users always see what they are typing.
func (m *FullscreenModel) renderInputArea() string {
	var b strings.Builder

	if m.layout != nil && !m.layout.ShouldUseBorder() {
		// Borderless single-line prompt for minimal terminals.
		b.WriteString(m.textarea.View())
		b.WriteString("\n")
	} else {
		innerWidth := m.termWidth - 4
		if m.layout != nil {
			innerWidth = m.layout.InputInnerWidth()
		}
		panel := fsInputPanelStyle(innerWidth).Render(m.textarea.View())
		b.WriteString(panel)
		b.WriteString("\n")
	}

	// File picker popup — rendered above the help line when active.
	if fp := m.renderFilePicker(); fp != "" {
		b.WriteString(fp)
		b.WriteString("\n")
	}

	// Help hint along the bottom.
	help := fsHelpStyle().Render(m.buildHelpText())
	b.WriteString(help)

	return b.String()
}

// renderMessage renders a single message in the Claude Code style — a left
// vertical colored bar plus a bold role label followed by the wrapped
// content — and caches the result.
func (m *FullscreenModel) renderMessage(msg *FormattedMessage) {
	borderColor := fsBorderColorForRole(msg.Role)
	label := fsLabelForRole(msg.Role)

	contentWidth := fsMessageContentWidth(m.termWidth)
	if m.layout != nil {
		contentWidth = m.layout.ContentWidth()
	}

	// Thinking messages get a distinct framed block so users can scan the
	// reasoning timeline at a glance. The body shows a single-line
	// collapsed summary by default; the full text is included in
	// scrollback dumps either way.
	if msg.Role == "thinking" {
		rendered := renderInlineThinkingBlock(msg.Content, contentWidth, m.termCap, msg.Collapsed)
		// Reserve a trailing blank line between bubbles.
		rendered += "\n"
		msg.SetRendered(fsThinkingBlockStyle(m.termWidth).Render(rendered))
		return
	}

	// Tool messages render from structured Metadata when available so
	// header / params / result are presented consistently and the
	// localized summary in msg.Content is never re-parsed.
	if msg.Role == "tool" {
		if rendered, ok := m.renderToolMessage(msg, label, borderColor, contentWidth); ok {
			msg.SetRendered(rendered)
			return
		}
	}

	header := fsRoleLabelStyle(borderColor).Render(fsHeaderLabel(msg.Role, label, m.termCap))

	// Wrap the body to the available content width. lipgloss handles soft
	// wrapping when Width is set on the surrounding bubble style, but we
	// also pre-wrap so the cached height is accurate.
	body := softWrap(msg.Content, contentWidth)

	inner := header + "\n" + body
	rendered := fsMessageBubbleStyle(borderColor, m.termWidth).Render(inner)

	// Add a trailing blank line between bubbles for breathing room.
	rendered += "\n"

	msg.SetRendered(rendered)
}

// renderToolMessage formats a tool message from its structured Metadata
// fields. Returns (rendered, true) when metadata is present; otherwise
// (",", false) so the caller falls back to the generic rendering. The
// collapsed-result summary keeps the first 5 lines and replaces the rest
// with a "... N more lines" hint; Ctrl+O toggles the collapsed flag.
func (m *FullscreenModel) renderToolMessage(msg *FormattedMessage, label, borderColor string, contentWidth int) (string, bool) {
	if msg.Metadata == nil {
		return "", false
	}
	name, hasName := metaString(msg.Metadata, "tool_name")
	status, hasStatus := metaString(msg.Metadata, "tool_status")
	params, hasParams := metaString(msg.Metadata, "tool_params")
	result, hasResult := metaString(msg.Metadata, "tool_result")
	if !hasName && !hasStatus && !hasParams && !hasResult {
		return "", false
	}

	prefix := SafeChar("tool_prefix", m.termCap)
	headerText := prefix + label
	if hasName {
		headerText += " " + name
	}
	if hasStatus {
		headerText += " [" + status + "]"
	}
	header := fsRoleLabelStyle(borderColor).Render(headerText)

	var b strings.Builder
	b.WriteString(header)
	if hasParams && strings.TrimSpace(params) != "" {
		b.WriteString("\nparams:\n")
		b.WriteString(softWrap(indentLines(params, "  "), contentWidth))
	}
	if hasResult && strings.TrimSpace(result) != "" {
		b.WriteString("\nresult:\n")
		body := result
		if msg.Collapsed {
			body = collapseResult(result, 5)
		}
		b.WriteString(softWrap(indentLines(body, "  "), contentWidth))
	}

	rendered := fsMessageBubbleStyle(borderColor, m.termWidth).Render(b.String())
	rendered += "\n"
	return rendered, true
}

// indentLines prefixes each line of s with the given indent. Empty input
// returns the empty string unchanged.
func indentLines(s, indent string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		parts[i] = indent + p
	}
	return strings.Join(parts, "\n")
}

// collapseResult returns a summary view of result keeping at most
// headLines lines from the top followed by a "... N more lines" hint.
// If the result already fits inside the limit it is returned unchanged.
func collapseResult(result string, headLines int) string {
	if headLines <= 0 {
		headLines = 1
	}
	lines := strings.Split(result, "\n")
	if len(lines) <= headLines {
		return result
	}
	more := len(lines) - headLines
	summary := strings.Join(lines[:headLines], "\n")
	return summary + fmt.Sprintf("\n... %d more lines", more)
}

// renderInlineThinkingBlock formats a thinking message as a small framed
// block embedded in the message stream. The previous design stacked
// thinking content into the status bar where it disappeared once the
// thinking phase ended; rendering it inline gives users a permanent
// record they can scroll back to.
//
// When collapsed is true, only the summary header and a "[N 字符, Ctrl+T
// 展开]" hint are returned. When collapsed is false, the full reasoning
// body is included along with a trailing "[Ctrl+T 折叠]" hint. The
// caller wires this up to FormattedMessage.Collapsed so each thinking
// block can be toggled independently via the Ctrl+T shortcut.
func renderInlineThinkingBlock(content string, width int, cap TermCapability, collapsed bool) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	// The recorded content has the form "思考完成 [N 字符]\n<reasoning>".
	// Split the summary from the body so we can render them differently.
	summary, body, hasBody := strings.Cut(content, "\n")
	prefix := SafeChar("thinking_prefix", cap)
	header := prefix + summary
	if !hasBody || strings.TrimSpace(body) == "" {
		return header
	}

	if collapsed {
		// Show only the summary with an expansion hint so users know
		// the body is hidden but recoverable via Ctrl+T.
		return header + "  [Ctrl+T 展开]"
	}

	// Soft-wrap the body and indent each line with a vertical bar so the
	// block reads as a distinct quote-like region.
	wrapped := softWrap(strings.TrimSpace(body), width-4)
	bar := "│ "
	if !cap.SupportsBoxDraw {
		bar = "| "
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	for i, line := range strings.Split(wrapped, "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(bar)
		b.WriteString(line)
	}
	b.WriteString("\n")
	b.WriteString(bar)
	b.WriteString("[Ctrl+T 折叠]")
	return b.String()
}

// softWrap wraps `s` to the given width on word boundaries when possible. It
// preserves explicit newlines.
func softWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(wrapLine(line, width))
	}
	return out.String()
}

func wrapLine(line string, width int) string {
	if width <= 0 || len([]rune(line)) <= width {
		return line
	}
	runes := []rune(line)
	var out strings.Builder
	for len(runes) > width {
		// Try to break at the last space within the window.
		brk := width
		for i := width - 1; i > 0; i-- {
			if runes[i] == ' ' {
				brk = i
				break
			}
		}
		out.WriteString(string(runes[:brk]))
		out.WriteByte('\n')
		// Skip the breaking space if any.
		if brk < len(runes) && runes[brk] == ' ' {
			runes = runes[brk+1:]
		} else {
			runes = runes[brk:]
		}
	}
	out.WriteString(string(runes))
	return out.String()
}

// addUserMessage adds a user message to the list.
func (m *FullscreenModel) addUserMessage(content string) {
	msg := NewFormattedMessage(fmt.Sprintf("user-%d", time.Now().UnixNano()), "user", content)
	m.renderMessage(msg)
	m.messages.AddMessage(msg)
	m.updateViewportHeight()
}

// handleIncomingMessage handles non-streaming incoming messages. assistant_stream
// chunks are handled directly in Update() via the streaming buffer; this method
// is only called for other roles (user, error, system, tool, assistant, etc.).
func (m *FullscreenModel) handleIncomingMessage(msg Message) {
	m.advanceSpinner("writing")
	// Regular message
	newMsg := NewFormattedMessage(fmt.Sprintf("%s-%d", msg.Role, time.Now().UnixNano()), msg.Role, msg.Content)
	m.renderMessage(newMsg)
	m.messages.AddMessage(newMsg)

	m.updateViewportHeight()
	if msg.Role == "assistant" {
		// Non-streaming assistant payloads represent a completed turn
		// from providers that don't emit a separate completion event.
		// Transition directly to phaseDone so the spinner stops.
		m.resetThinkingState()
		m.activeToolCalls = make(map[string]string)
		m.resetSpinnerToIdle()
		m.currentPhase = phaseDone
		m.status = m.formatStatusForPhase(phaseDone, "")
	}
}

// shouldFlushPartial returns true when the streaming buffer has content and
// the throttle interval has elapsed — mirrors the inline model's logic.
func (m *FullscreenModel) shouldFlushPartial() bool {
	return m.streamingBuf.Len() > 0 && time.Since(m.lastStreamFlush) >= streamFlushInterval
}

// flushStreamingIncremental appends the buffered content to the active
// streaming message without terminating the streaming state. Called on
// each SpinnerTickMsg when shouldFlushPartial() is true.
func (m *FullscreenModel) flushStreamingIncremental() {
	if !m.isStreaming || m.streamingBuf.Len() == 0 {
		return
	}
	chunk := m.streamingBuf.String()
	m.streamingBuf.Reset()
	m.lastStreamFlush = time.Now()

	m.advanceSpinner("writing")
	if last := m.messages.Last(); last != nil && last.Role == "assistant_stream" {
		last.Content += chunk
		m.renderMessage(last)
	} else {
		newMsg := NewFormattedMessage(fmt.Sprintf("assistant-%d", time.Now().UnixNano()), "assistant_stream", chunk)
		m.renderMessage(newMsg)
		m.messages.AddMessage(newMsg)
	}
	m.setAgentStatus(phaseResponse, "")
	m.updateViewportHeight()
}

// flushStreamingBuffer flushes all remaining buffered content and ends the
// streaming state. Called before any transition event (StatusUpdate,
// ThinkingMsg, ToolUseMsg, TaskCompletionMsg) to ensure content is committed.
func (m *FullscreenModel) flushStreamingBuffer() {
	if !m.isStreaming || m.streamingBuf.Len() == 0 {
		m.isStreaming = false
		return
	}
	chunk := m.streamingBuf.String()
	m.streamingBuf.Reset()
	m.isStreaming = false
	m.lastStreamFlush = time.Now()

	m.advanceSpinner("writing")
	if last := m.messages.Last(); last != nil && last.Role == "assistant_stream" {
		last.Content += chunk
		m.renderMessage(last)
	} else {
		newMsg := NewFormattedMessage(fmt.Sprintf("assistant-%d", time.Now().UnixNano()), "assistant_stream", chunk)
		m.renderMessage(newMsg)
		m.messages.AddMessage(newMsg)
	}
	m.setAgentStatus(phaseResponse, "")
	m.updateViewportHeight()
}

// handleThinkingMessage handles thinking messages.
func (m *FullscreenModel) handleThinkingMessage(msg ThinkingMsg) {
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
	if msg.Title != "" {
		m.thinkingTitle = msg.Title
	}
	if isNewSession && (msg.Reasoning != "" || msg.ReasoningDelta != "") {
		m.thinkingReasoning = ""
		m.thinkingPending = ""
		m.thinkingWindow = nil
	}
	if msg.Reasoning != "" {
		m.thinkingReasoning = msg.Reasoning
		m.updateThinkingWindow(m.buildThinkingPreview())
	}
	if msg.ReasoningDelta != "" {
		m.appendThinkingDelta(msg.ReasoningDelta)
	}

	m.advanceSpinner("thinking")
	if m.selectedVerb == "" {
		m.selectedVerb = spinnerverbs.RandomVerb(m.spinnerVerbs)
	}
	if isComplete {
		runes := []rune(m.thinkingReasoning)
		summaryContent := fmt.Sprintf("思考完成 [%d 字符]", len(runes))
		// Skip empty thinking blocks so providers that emit "is_complete"
		// without any accumulated reasoning don't render a "思考完成 [0
		// 字符]" placeholder in the message stream.
		if strings.TrimSpace(m.thinkingReasoning) != "" {
			storeContent := summaryContent + "\n" + m.thinkingReasoning
			m.recordThinkingMessage(storeContent)
			// Preserve a brief preview so the user notices the thinking
			// finished even after the assistant stream takes over. The
			// preview is shown in the status area; the full body is
			// available in the MessageStore and via Ctrl+T.
			m.thinkingWindow = []string{summaryContent + "，Ctrl+T 展开"}
		} else {
			m.thinkingWindow = nil
		}
		m.thinkingPending = ""
	} else {
		m.setAgentStatus(phaseThinking, "")
	}
}

// handleToolUseMessage handles tool use messages. The visible body is a
// brief one-line summary kept for backward compatibility, but the full
// structured tool data (name, status, params, result) is stashed on
// FormattedMessage.Metadata so the milktea renderer can build a rich,
// collapsible block from the structured fields and the dump path can
// preserve every field verbatim.
func (m *FullscreenModel) handleToolUseMessage(msg ToolUseMsg) {
	m.advanceSpinner("executing")
	if m.currentPhase == phaseThinking {
		m.currentPhase = phaseToolCall
	}
	lastStatus := m.activeToolCalls[msg.ID]
	var summary string
	emit := false
	switch msg.Status {
	case "executing":
		if lastStatus == "" {
			summary = fmt.Sprintf("正在调用工具: %s", msg.ToolName)
			emit = true
		}
	case "success":
		if lastStatus != "success" {
			resultPreview := truncateResult(strings.ReplaceAll(msg.Result, "\n", " "), 200)
			summary = fmt.Sprintf("工具: %s 调用完成\n结果: %s", msg.ToolName, resultPreview)
			emit = true
		}
	case "error":
		summary = fmt.Sprintf("工具 %s 执行失败: %s", msg.ToolName, msg.Result)
		emit = true
	case "cancelled":
		if lastStatus != "cancelled" {
			summary = fmt.Sprintf("工具 %s 已取消", msg.ToolName)
			emit = true
		}
	}
	if emit {
		role := "tool"
		if msg.Status == "error" {
			role = "error"
		}
		fm := NewFormattedMessage(fmt.Sprintf("%s-%d", role, time.Now().UnixNano()), role, summary)
		if fm.Metadata == nil {
			fm.Metadata = make(map[string]interface{})
		}
		fm.Metadata["tool_name"] = msg.ToolName
		fm.Metadata["tool_status"] = msg.Status
		if msg.Params != nil {
			fm.Metadata["tool_params"] = msg.Params
		}
		if msg.Result != "" {
			fm.Metadata["tool_result"] = msg.Result
		}
		// Tool results default to collapsed so long outputs don't
		// dominate the message stream; Ctrl+O toggles the latest.
		if role == "tool" {
			fm.Collapsed = true
		}
		m.renderMessage(fm)
		m.messages.AddMessage(fm)
		m.updateViewportHeight()
	}

	switch msg.Status {
	case "success", "error", "cancelled":
		delete(m.activeToolCalls, msg.ID)
	default:
		m.activeToolCalls[msg.ID] = msg.Status
	}

	switch msg.Status {
	case "executing":
		detail := msg.ToolName
		if len(m.activeToolCalls) > 1 {
			detail = ""
		}
		m.setAgentStatus(phaseToolCall, detail)
	case "success", "cancelled":
		if len(m.activeToolCalls) > 0 {
			m.setAgentStatus(phaseToolCall, "")
		} else {
			m.setAgentStatus(phaseProcessing, "")
		}
	case "error":
		m.setAgentStatus(phaseToolCall, msg.ToolName+" 失败")
	}
	if m.thinkingReasoning != "" && len(m.thinkingWindow) == 0 {
		m.updateThinkingWindow(m.buildThinkingPreview())
	}
}

func (m *FullscreenModel) recordMessage(role, content string) {
	newMsg := NewFormattedMessage(fmt.Sprintf("%s-%d", role, time.Now().UnixNano()), role, content)
	m.renderMessage(newMsg)
	m.messages.AddMessage(newMsg)
	m.updateViewportHeight()
}

// recordThinkingMessage appends a thinking message with Collapsed=true so
// finished reasoning blocks are summarised by default. Users can expand
// them with Ctrl+T (handled per-message via FormattedMessage.Collapsed).
func (m *FullscreenModel) recordThinkingMessage(content string) {
	newMsg := NewFormattedMessage(fmt.Sprintf("thinking-%d", time.Now().UnixNano()), "thinking", content)
	newMsg.Collapsed = true
	m.renderMessage(newMsg)
	m.messages.AddMessage(newMsg)
	m.updateViewportHeight()
}

// lastThinkingMessage returns the most recent thinking message in the
// store, or nil if none exists. Used by the Ctrl+T shortcut so toggling
// affects exactly one block at a time. Iterates from the tail of the
// store so the lookup short-circuits on the first match.
func (m *FullscreenModel) lastThinkingMessage() *FormattedMessage {
	for i := m.messages.Len() - 1; i >= 0; i-- {
		msg := m.messages.Get(i)
		if msg != nil && msg.Role == "thinking" {
			return msg
		}
	}
	return nil
}

// lastToolMessage returns the most recent tool message in the store, or
// nil if none exists. Used by the Ctrl+O shortcut so toggling only
// affects the latest tool block.
func (m *FullscreenModel) lastToolMessage() *FormattedMessage {
	for i := m.messages.Len() - 1; i >= 0; i-- {
		msg := m.messages.Get(i)
		if msg != nil && msg.Role == "tool" {
			return msg
		}
	}
	return nil
}

func (m *FullscreenModel) handleStatusUpdate(msg StatusUpdate) {
	switch string(msg) {
	case "完成":
		m.resetThinkingState()
		m.activeToolCalls = make(map[string]string)
		m.resetSpinnerToIdle()
		m.currentPhase = phaseDone
		m.status = m.formatStatusForPhase(phaseDone, "")
	case "等待输入":
		m.resetThinkingState()
		m.resetSpinnerToIdle()
		m.currentPhase = phaseIdle
		m.status = m.formatStatusForPhase(phaseIdle, "")
	default:
		if !m.isActivePhase() {
			m.status = string(msg)
		}
	}
}

func (m *FullscreenModel) handleTokenStatsUpdate(msg TokenStatsUpdate) {
	m.advanceSpinner("")
	in := formatCount(msg.InputTokens)
	out := formatCount(msg.OutputTokens)
	total := formatCount(msg.TotalTokens)
	tokens := fmt.Sprintf("输入 %s | 输出 %s | 总计 %s", in, out, total)
	if msg.Peak > 0 {
		tokens += fmt.Sprintf(" | 峰值速率: %.2f t/s", msg.Peak)
	}
	effMax, effUsed := msg.ContextWindowMax, msg.ContextUsedTokens
	if effMax <= 0 && m.contextWindowMax > 0 {
		effMax, effUsed = m.contextWindowMax, m.contextUsedTokens
	}
	if effMax > 0 {
		pct := float64(effUsed) / float64(effMax) * 100
		tokens += fmt.Sprintf(" | 上下文: %s/%s (%.0f%%)", formatCount(effUsed), formatCount(effMax), pct)
	}
	m.tokenStatus = tokens
	if msg.ContextWindowMax > 0 {
		m.contextWindowMax = msg.ContextWindowMax
	}
	if msg.ContextUsedTokens > 0 {
		m.contextUsedTokens = msg.ContextUsedTokens
	}
}

func (m *FullscreenModel) appendThinkingDelta(delta string) {
	if delta == "" {
		return
	}
	normalized := strings.ReplaceAll(delta, "\n", " ")
	if strings.TrimSpace(normalized) == "" {
		return
	}
	m.thinkingReasoning += delta
	m.thinkingPending += normalized
	rawPending := m.thinkingPending

	width := m.thinkingWrapWidth()
	wrapped := wordwrap.String(m.thinkingPending, width)
	lines := strings.Split(wrapped, "\n")
	displayLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			displayLines = append(displayLines, line)
		}
	}
	if len(displayLines) > 3 {
		m.thinkingWindow = displayLines[len(displayLines)-3:]
	} else {
		m.thinkingWindow = displayLines
	}
	if n := len(lines); n > 0 {
		m.thinkingPending = lines[n-1]
		if strings.HasSuffix(rawPending, " ") && !strings.HasSuffix(m.thinkingPending, " ") {
			m.thinkingPending += " "
		}
	}
}

func (m *FullscreenModel) updateThinkingWindow(preview string) {
	normalized := strings.TrimSpace(strings.ReplaceAll(preview, "\n", " "))
	if normalized == "" {
		return
	}
	lines := strings.Split(wordwrap.String(normalized, m.thinkingWrapWidth()), "\n")
	displayLines := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			displayLines = append(displayLines, line)
		}
	}
	if len(displayLines) > 3 {
		displayLines = displayLines[len(displayLines)-3:]
	}
	m.thinkingWindow = displayLines
	m.thinkingPending = normalized
}

func (m *FullscreenModel) thinkingWrapWidth() int {
	width := m.termWidth - thinkingWindowPrefixReserve
	if width < minThinkingWindowWidth {
		width = minThinkingWindowWidth
	}
	return width
}

func (m *FullscreenModel) buildThinkingPreview() string {
	if m.thinkingReasoning == "" {
		return ""
	}
	runes := []rune(m.thinkingReasoning)
	if len(runes) > 80 {
		runes = runes[len(runes)-80:]
	}
	return strings.TrimSpace(strings.ReplaceAll(string(runes), "\n", " "))
}

func (m *FullscreenModel) expandedThinkingLines() []string {
	lines := strings.Split(wordwrap.String(m.thinkingReasoning, m.thinkingWrapWidth()), "\n")
	maxShow := len(lines)
	if m.termHeight > 0 {
		limit := m.termHeight / 3
		if limit < 1 {
			limit = 1
		}
		if maxShow > limit {
			maxShow = limit
		}
	}
	start := len(lines) - maxShow
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, maxShow)
	for _, line := range lines[start:] {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (m *FullscreenModel) resetThinkingState() {
	m.thinkingTitle = ""
	m.thinkingReasoning = ""
	m.thinkingCompleted = false
	m.thinkingWindow = nil
	m.thinkingPending = ""
}

func (m *FullscreenModel) resetSpinnerToIdle() {
	m.spinnerStage = ""
	m.spinnerFrame = 0
	m.selectedVerb = ""
}

func (m *FullscreenModel) advanceSpinner(stage string) {
	if stage != "" {
		m.spinnerStage = stage
	}
	m.spinnerFrame++
}

func (m *FullscreenModel) currentSpinnerFrame() string {
	if !m.isSpinnerPhase() {
		return ""
	}
	return SafeSpinnerFrame(m.spinnerFrame, m.termCap)
}

// currentSpinnerVerb returns the current spinner verb for the active thinking cycle.
func (m *FullscreenModel) currentSpinnerVerb() string {
	if !m.isSpinnerPhase() {
		return ""
	}
	return m.selectedVerb
}

func (m *FullscreenModel) isSpinnerPhase() bool {
	return m.isActivePhase() || m.currentPhase == phaseProcessing || m.currentPhase == phaseResponse
}

func (m *FullscreenModel) isActivePhase() bool {
	switch m.currentPhase {
	case phaseThinking, phaseToolCall, phaseAwaitingApproval:
		return true
	default:
		return false
	}
}

func (m *FullscreenModel) setAgentStatus(phase displayPhase, detail string) {
	if (phase == phaseIdle || phase == phaseDone) && m.isActivePhase() {
		return
	}
	m.currentPhase = phase
	m.status = m.formatStatusForPhase(phase, detail)
}

func (m *FullscreenModel) formatStatusForPhase(phase displayPhase, detail string) string {
	switch phase {
	case phaseIdle:
		return "等待输入"
	case phaseProcessing:
		m.spinnerStage = "thinking"
		return "处理中..."
	case phaseThinking:
		m.spinnerStage = "thinking"
		if detail != "" {
			return "思考中... " + detail
		}
		return "思考中..."
	case phaseToolCall:
		m.spinnerStage = "executing"
		n := len(m.activeToolCalls)
		if n <= 1 {
			if detail != "" {
				return "工具执行中: " + detail
			}
			return "工具执行中..."
		}
		return fmt.Sprintf("工具执行中 (%d 个并行)", n)
	case phaseAwaitingApproval:
		if detail != "" {
			return "等待用户确认: " + detail
		}
		return "等待用户确认"
	case phaseResponse:
		m.spinnerStage = "writing"
		return "正在回复..."
	case phaseDone:
		return SafeChar("success", m.termCap) + " 完成"
	default:
		return ""
	}
}

func (m *FullscreenModel) renderContextBar() string {
	if m.contextWindowMax <= 0 {
		if m.contextUsedTokens > 0 {
			return fmt.Sprintf("ctx: %s", formatCount(m.contextUsedTokens))
		}
		return ""
	}
	pct := float64(m.contextUsedTokens) / float64(m.contextWindowMax)
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	width := 10
	filled := int(pct * float64(width))
	bar := SafeProgressBar(filled, width, m.termCap)
	return fmt.Sprintf("[%s] %d%%", bar, int(pct*100))
}

func (m *FullscreenModel) lastAssistantReply() string {
	for i := m.messages.Len() - 1; i >= 0; i-- {
		msg := m.messages.Get(i)
		if msg != nil && (msg.Role == "assistant" || msg.Role == "assistant_stream") {
			return msg.Content
		}
	}
	return ""
}

func (m *FullscreenModel) clearSessionCmd() tea.Cmd {
	newID := m.ClearSession()
	msg := "已开启新会话"
	if newID != "" {
		msg = fmt.Sprintf("已开启新会话 (id: %s)", newID)
	}
	m.recordMessage("system", msg)
	cmds := []tea.Cmd{tea.ClearScreen}
	if m.outbound != nil {
		cmds = append(cmds, m.outboundCmd(eventsource.Outbound{Kind: "control", Control: "/clear"}))
	}
	return tea.Sequence(cmds...)
}

func (m *FullscreenModel) ClearSession() string {
	m.messages = NewMessageStore()
	m.resetThinkingState()
	m.activeToolCalls = make(map[string]string)
	m.resetSpinnerToIdle()
	m.swarmLine = ""
	m.contextUsedTokens = 0
	m.contextWindowMax = 0
	m.tokenStatus = ""
	m.currentPhase = phaseIdle
	m.status = m.formatStatusForPhase(phaseIdle, "")
	m.textarea.Reset()
	m.historyIndex = -1
	m.inputDraft = ""
	m.hideConfirmation()
	if m.newSessionHandler != nil {
		return m.newSessionHandler()
	}
	return ""
}

func (m *FullscreenModel) outboundCmd(o eventsource.Outbound) tea.Cmd {
	if m.outbound == nil {
		return nil
	}
	send := m.outbound
	return func() tea.Msg {
		_ = send(o)
		return nil
	}
}

func (m *FullscreenModel) handleTabCompletion() {
	val := m.textarea.Value()
	if !strings.HasPrefix(val, "/") {
		return
	}
	// Prefer slash-command completion when the slash registry has been
	// loaded (via the command palette). This matches the inline mode's
	// behavior of completing /<partial> against the full registry.
	prefix := strings.TrimPrefix(val, "/")
	if sp := strings.IndexAny(prefix, " \t"); sp >= 0 {
		prefix = prefix[:sp]
	}
	if prefix != "" {
		names := m.slashNames
		if len(names) == 0 {
			// Lazy-load the registry so Tab completion works even before
			// the user opens the palette.
			m.loadCommands()
			names = m.slashNames
		}
		if match := uniquePrefixMatch(names, prefix); match != "" {
			rest := strings.TrimPrefix(val, "/"+prefix)
			m.textarea.SetValue("/" + match + rest)
			m.textarea.CursorEnd()
			return
		}
		if candidates := allPrefixMatches(names, prefix); len(candidates) > 1 {
			m.recordMessage("system", "命令候选："+strings.Join(candidates, "  "))
			return
		}
	}
	// Fall back to the optional model lister (legacy behavior).
	if m.modelLister == nil {
		return
	}
	models := strings.Fields(m.modelLister())
	if len(models) == 0 {
		return
	}
	m.recordMessage("system", "补全候选："+strings.Join(models, "  "))
}

func (m *FullscreenModel) handleConfirmationKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "left", "h":
		if m.confirmationSelected > 0 {
			m.confirmationSelected--
		}
	case "right", "l":
		if m.confirmationSelected < 2 {
			m.confirmationSelected++
		}
	case "enter":
		cmd := m.buildConfirmationCmd()
		m.hideConfirmation()
		return cmd
	case "ctrl+c", "q", "esc":
		callback := m.confirmationCallback
		m.hideConfirmation()
		return func() tea.Msg {
			if callback != nil {
				callback(false)
			}
			return nil
		}
	}
	return nil
}

func (m *FullscreenModel) buildConfirmationCmd() tea.Cmd {
	callback := m.confirmationCallback
	alwaysCallback := m.confirmationAlwaysCallback
	selected := m.confirmationSelected
	return func() tea.Msg {
		switch selected {
		case 0:
			if callback != nil {
				callback(true)
			}
		case 1:
			if callback != nil {
				callback(false)
			}
		case 2:
			if alwaysCallback != nil {
				alwaysCallback()
			} else if callback != nil {
				callback(true)
			}
		}
		return nil
	}
}

func (m *FullscreenModel) hideConfirmation() {
	m.showingConfirmation = false
	m.confirmationMessage = ""
	m.confirmationToolInfo = nil
	m.confirmationCallback = nil
	m.confirmationAlwaysCallback = nil
	m.confirmationSelected = 0
	m.confirmationButtons = nil
	if m.currentPhase == phaseAwaitingApproval {
		m.currentPhase = phaseProcessing
		m.status = m.formatStatusForPhase(phaseProcessing, "")
	}
}

func (m *FullscreenModel) renderConfirmationDialog(b *strings.Builder) {
	m.confirmationButtons = nil
	title := fsConfirmationTitleStyle().Render("工具执行确认")
	b.WriteString(title + "\n\n")
	b.WriteString(fsConfirmationMessageStyle().Render(m.confirmationMessage) + "\n\n")
	if m.confirmationToolInfo != nil {
		if toolName, ok := m.confirmationToolInfo["Name"].(string); ok {
			b.WriteString(fsConfirmationInfoStyle().Render("工具: "+toolName) + "\n")
		}
		if params, ok := m.confirmationToolInfo["Parameters"]; ok {
			b.WriteString(fsConfirmationInfoStyle().Render("参数: ") + "\n")
			if paramsMap, ok := params.(map[string]interface{}); ok {
				for key, value := range paramsMap {
					b.WriteString(fsConfirmationInfoStyle().Render(truncatePlain(fmt.Sprintf("  %s: %v", key, value), 80)) + "\n")
				}
			} else {
				b.WriteString(fsConfirmationInfoStyle().Render(truncatePlain(fmt.Sprintf("  %v", params), 80)) + "\n")
			}
		}
		b.WriteString("\n")
	}

	yesButton := fsConfirmationButtonStyle(false, "yes").Render("确认")
	noButton := fsConfirmationButtonStyle(false, "no").Render("取消")
	alwaysButton := fsConfirmationButtonStyle(false, "always").Render("始终允许")
	switch m.confirmationSelected {
	case 0:
		yesButton = fsConfirmationButtonStyle(true, "yes").Render("确认")
	case 1:
		noButton = fsConfirmationButtonStyle(true, "no").Render("取消")
	case 2:
		alwaysButton = fsConfirmationButtonStyle(true, "always").Render("始终允许")
	}
	buttonY := strings.Count(b.String(), "\n")
	yesWidth := lipgloss.Width(yesButton)
	noWidth := lipgloss.Width(noButton)
	m.confirmationButtons = []hitBox{
		{x0: 0, x1: yesWidth, y: buttonY},
		{x0: yesWidth + 2, x1: yesWidth + 2 + noWidth, y: buttonY},
		{x0: yesWidth + 2 + noWidth + 2, x1: yesWidth + 2 + noWidth + 2 + lipgloss.Width(alwaysButton), y: buttonY},
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, yesButton, "  ", noButton, "  ", alwaysButton) + "\n\n")
	b.WriteString(fsHelpStyle().Render("← → 或 h l 选择，Enter 确认，Esc 取消") + "\n")
}

func (m *FullscreenModel) hitTestConfirmationButton(x, y int) int {
	for i, box := range m.confirmationButtons {
		if y == box.y && x >= box.x0 && x < box.x1 {
			return i
		}
	}
	return -1
}

func (m *FullscreenModel) renderPermissionModeTag() string {
	if m.permissionManager == nil {
		return ""
	}
	mode := m.permissionMode
	if mode == "" {
		mode = "default"
	}
	if permission.PermissionMode(mode) == permission.ModeDefault {
		return ""
	}
	return fmt.Sprintf("%s %s", m.permissionModeIcon(mode), mode)
}

func (m *FullscreenModel) permissionModeIcon(mode string) string {
	switch permission.PermissionMode(mode) {
	case permission.ModeYOLO:
		return "⚡"
	case permission.ModeAuto:
		return "🤖"
	case permission.ModePlan:
		return "📋"
	case permission.ModeAcceptEdits:
		return "✎"
	default:
		return "🛡"
	}
}

func (m *FullscreenModel) buildHelpText() string {
	if !m.showHelp {
		hint := "? 帮助"
		if m.termWidth > 0 && lipgloss.Width(hint) <= m.termWidth {
			return hint
		}
		return ""
	}
	full := "Enter 发送 · Shift+Enter/Ctrl+J 换行 · Ctrl+T 思考 · Ctrl+Y 复制 · Ctrl+L 新会话 · Ctrl+Z 取消 · Ctrl+P 命令 · Ctrl+R 历史 · Ctrl+F 搜索 · [ 滚动 · PgUp/PgDn 翻页 · Tab 补全 · Shift+Tab 权限 · ? 收起"
	if m.termWidth <= 0 {
		return ""
	}
	if lipgloss.Width(full) <= m.termWidth {
		return full
	}
	short := "Enter 发送 · ^J 换行 · ^T 思考 · ^Y 复制 · ^L 新会话 · ^Z 取消 · ^R 历史 · ^F 搜索 · PgUp/PgDn · Tab 补全 · ? 收起"
	if lipgloss.Width(short) <= m.termWidth {
		return short
	}
	tiny := "Enter发送 · ^J换行 · ^T思考 · ^F搜索 · ^L新会话 · PgUp/PgDn · ?收起"
	if lipgloss.Width(tiny) <= m.termWidth {
		return tiny
	}
	micro := "Enter发送 · ^L新会话 · ?收起"
	if lipgloss.Width(micro) <= m.termWidth {
		return micro
	}
	return xansi.Truncate(full, m.termWidth, "…")
}

func (m *FullscreenModel) updateLayoutHeights() {
	statusBarHeight := renderedLineCount(m.renderStatusBar())
	inputHeight := m.textarea.Height() + 2
	helpHeight := 1
	m.viewport.SetViewportHeight(m.termHeight - statusBarHeight - inputHeight - helpHeight)
}

func truncatePlain(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if xansi.StringWidth(s) <= width {
		return s
	}
	return xansi.Truncate(s, width, "")
}

func isContinuationInput(value string) bool {
	return strings.HasSuffix(value, "\\") && !strings.HasSuffix(value, "\\\\")
}

// updateViewportHeight recalculates total height and updates viewport.
func (m *FullscreenModel) updateViewportHeight() {
	m.updateLayoutHeights()
	totalHeight := 0
	m.messages.Range(func(_ int, msg *FormattedMessage) bool {
		totalHeight += msg.Height
		return true
	})
	m.viewport.SetTotalHeight(totalHeight)
}

// BindOutbound binds the outbound message handler.
func (m *FullscreenModel) BindOutbound(fn func(eventsource.Outbound) error) {
	m.outbound = fn
}

// SetPermissionManager wires the permission manager. Stored for symmetry with
// the inline model; the fullscreen view does not yet render the permission
// dialog itself — that port is tracked as a separate piece of work.
func (m *FullscreenModel) SetPermissionManager(mgr *permission.Manager) {
	m.permissionManager = mgr
	if mgr != nil {
		m.permissionMode = string(mgr.GetMode())
	}
}

// PermissionManager returns the wired permission manager (may be nil).
func (m *FullscreenModel) PermissionManager() *permission.Manager {
	return m.permissionManager
}

// SetModelLister wires the slash-command model lister. Stored for symmetry
// with the inline model.
func (m *FullscreenModel) SetModelLister(fn func() string) {
	m.modelLister = fn
}

// ModelLister returns the wired slash-command model lister (may be nil).
func (m *FullscreenModel) ModelLister() func() string {
	return m.modelLister
}

// SetNewSessionHandler wires the callback used by Ctrl+L to create a new agent session.
func (m *FullscreenModel) SetNewSessionHandler(h func() string) {
	m.newSessionHandler = h
}

// NewSessionHandler returns the wired Ctrl+L / new-session handler (may be nil).
func (m *FullscreenModel) NewSessionHandler() func() string {
	return m.newSessionHandler
}

// SetSkillLister wires the /skills slash-command callback.
func (m *FullscreenModel) SetSkillLister(fn func() string) { m.skillLister = fn }

// SetModelStatusGetter wires the /models status callback.
func (m *FullscreenModel) SetModelStatusGetter(fn func() string) { m.modelStatusGetter = fn }

// SetModelSwitcher wires the /models switch callback.
func (m *FullscreenModel) SetModelSwitcher(fn func(string) string) { m.modelSwitcher = fn }

// SetModelFallbackHandler wires the model fallback callback.
func (m *FullscreenModel) SetModelFallbackHandler(fn func(string) string) {
	m.modelFallbackHandler = fn
}

// SetModelDoctor wires the /models doctor callback.
func (m *FullscreenModel) SetModelDoctor(fn func(string) string) { m.modelDoctor = fn }

// SetContextStatusGetter wires the /context status callback.
func (m *FullscreenModel) SetContextStatusGetter(fn func() string) { m.contextStatusGetter = fn }

// SetDoctorReporter wires the /doctor callback.
func (m *FullscreenModel) SetDoctorReporter(fn func() string) { m.doctorReporter = fn }

// SetEventsQuerier wires the /events callback.
func (m *FullscreenModel) SetEventsQuerier(fn func(string) string) { m.eventsQuerier = fn }

// SetAuditQuerier wires the /audit callback.
func (m *FullscreenModel) SetAuditQuerier(fn func(string) string) { m.auditQuerier = fn }

// SetRoutinesLister wires the /routines list callback.
func (m *FullscreenModel) SetRoutinesLister(fn func() string) { m.routinesLister = fn }

// SetRunningStatusLister wires the /routines status callback.
func (m *FullscreenModel) SetRunningStatusLister(fn func() string) { m.runningStatusLister = fn }

// SetRoutinesAdder wires the /routines add callback.
func (m *FullscreenModel) SetRoutinesAdder(fn func(string) string) { m.routinesAdder = fn }

// SetRoutinesRemover wires the /routines remove callback.
func (m *FullscreenModel) SetRoutinesRemover(fn func(string) string) { m.routinesRemover = fn }

// SetRoutinesPauser wires the /routines pause callback.
func (m *FullscreenModel) SetRoutinesPauser(fn func(string) string) { m.routinesPauser = fn }

// SetRoutinesResumer wires the /routines resume callback.
func (m *FullscreenModel) SetRoutinesResumer(fn func(string) string) { m.routinesResumer = fn }

// SetRoutinesRunner wires the /routines run callback.
func (m *FullscreenModel) SetRoutinesRunner(fn func(string) string) { m.routinesRunner = fn }

// SetAllowlistHandler wires the "始终允许" allowlist callback.
func (m *FullscreenModel) SetAllowlistHandler(h func(toolName string, params map[string]interface{})) {
	m.allowlistHandler = h
}

// SetTeamName scopes /teammates and /agents slash commands to the given team.
func (m *FullscreenModel) SetTeamName(name string) { m.teamName = name }

// SetAvailableToolNames provides tool names for Tab completion of /allow <tool>.
func (m *FullscreenModel) SetAvailableToolNames(names []string) { m.availableToolNames = names }

// SetPersistentAllowlist enables /disallow to remove rules from persistent storage.
func (m *FullscreenModel) SetPersistentAllowlist(store *permission.PersistentAllowlistStore, workdir string) {
	m.persistentAllowlist = store
	m.workdir = workdir
}

// SetEngine enables engine-level slash commands (/think etc.).
func (m *FullscreenModel) SetEngine(eng *engine.Engine) { m.engine = eng }

// SetAttachmentManager sets the attachment manager for the fullscreen model
func (m *FullscreenModel) SetAttachmentManager(mgr *attachment.Manager) {
	m.attachmentMgr = mgr
}
