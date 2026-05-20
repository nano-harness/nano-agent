package bubbletea

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/nano-harness/nano-agent/pkg/agent/permission"
	"github.com/nano-harness/nano-agent/pkg/attachment"
	"github.com/nano-harness/nano-agent/pkg/clipboard"
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

// ── Semantic color palette ───────────────────────────────────────────────────
// Inspired by GitHub Copilot CLI's semantic color role design.
// Uses 256-color ANSI codes chosen for readability on dark terminal backgrounds.
const (
	colorAssistant      = paletteAssistant // sage green  – AI responses
	colorUser           = paletteUser      // soft blue   – user messages
	colorTool           = paletteTool      // light gray  – tool/secondary info
	colorError          = paletteError     // coral red   – errors
	colorSystem         = paletteSystem    // warm gold   – system messages
	colorStatus         = "73"             // Soft teal         – status/token info
	colorSuccess        = "114"            // Soft green        – success feedback
	colorWarning        = "215"            // Soft orange       – warnings/confirmation titles
	colorMuted          = "245"            // Medium gray       – separators, help text, borders
	colorDetail         = "252"            // Light gray        – detail/description text
	colorBright         = "255"            // Near-white        – important message text
	colorInfoTitle      = "75"             // Soft blue         – info panel titles
	colorDimBg          = "238"            // Dark gray         – subtle backgrounds (slightly lighter than 236)
	colorConfirmBg      = "236"            // Dark gray         – confirmation dialog title bg
	colorSecondary      = "248"            // Lighter gray      – secondary/tool info text, inactive button fg
	colorDefaultFg      = "250"            // Light gray        – default permission mode badge foreground
	colorYoloBg         = "208"            // Deep orange       – YOLO mode badge background
	colorOpenSpec       = "135"            // Soft purple       – OpenSpec category label
	colorOnAccent       = "0"              // Black             – foreground on colored badge backgrounds
	colorAcceptEditsBg  = "220"            // Amber yellow      – AcceptEdits permission badge background
	colorButtonFg       = "15"             // Bright white      – confirmation dialog button foreground
	colorYesButtonBg    = "28"             // Forest green      – "confirm" button background
	colorNoButtonBg     = "124"            // Dark red          – "cancel" button background
	colorAlwaysButtonBg = "33"             // Ocean blue        – "always allow" button background
)

// commandsPaletteVisibleRows is the number of command rows rendered at once in
// the command palette. Both Update() and renderCommandsPalette() must use this
// constant so the scrolling window stays consistent.
const commandsPaletteVisibleRows = 15

// commandsPaletteScrollPadding controls the minimum number of rows between the
// selected item and the top/bottom edges of the visible window. When the selection
// comes within this many rows of the edge, the viewport scrolls to keep the
// selection comfortably positioned (avoiding "edge-hugging" behavior).
const commandsPaletteScrollPadding = 3

const maxInputHeight = 8
const minInputHeight = 3
const inputContentMargin = 4
const maxFilePickerResults = 10

const (
	minMarkdownRenderWidth      = 40
	defaultMarkdownRenderWidth  = 100
	thinkingWindowPrefixReserve = 8
	minThinkingWindowWidth      = 20
	formattedLinePrefixReserve  = 4
	largePasteBytes             = 500
	largePasteLines             = 10
	streamFlushInterval         = 100 * time.Millisecond
	historyWheelScrollLines     = 3
)

const spinnerTickInterval = 100 * time.Millisecond

var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"} //nolint:gochecknoglobals

// -- Model --

// displayPhase represents the current content display phase
type displayPhase int

const (
	phaseIdle             displayPhase = iota
	phaseProcessing                    // 已提交，等待首个 thinking/tool/content 事件
	phaseThinking                      // 正在展示推理内容
	phaseToolCall                      // 正在展示工具调用
	phaseAwaitingApproval              // 等待用户确认工具
	phaseResponse                      // 正在展示最终回复
	phaseDone                          // 整个 turn 完成
)

type Model struct { //nolint:revive
	// Channels
	SubmitCh chan<- string
	CancelCh chan<- struct{}
	outbound func(eventsource.Outbound) error

	// Properties
	// messages is the shared declarative message store powering both the
	// sliding-window View() renderer and the fullscreen history view.
	messages         *MessageStore
	status           string
	cronIndicator    string
	termWidth        int
	termHeight       int
	apiBaseURL       string
	cwd              string
	keyboardEnhanced bool
	terminalFocused  bool

	// bannerArt holds the rendered static product banner (multi-line, no
	// trailing newline). When non-empty and the conversation has no messages
	// yet, View() renders it above the input area so the TUI always shows a
	// product mark while idle. Populated by the UI adapter after the startup
	// banner animation completes; left empty when --no-banner is set or on
	// non-interactive terminals.
	bannerArt string

	// History buffer management
	sessionStartTime time.Time // Session start timestamp

	// Components
	input textarea.Model

	// Confirmation dialog state
	showingConfirmation        bool
	confirmationMessage        string
	confirmationToolInfo       map[string]interface{}
	confirmationCallback       func(bool)
	confirmationAlwaysCallback func() // called instead of callback(true) when "始终允许" is chosen
	confirmationSelected       int    // 0 = Yes, 1 = No, 2 = Always (allowlist)
	confirmationIsPaste        bool
	pendingPaste               string
	confirmationButtons        []hitBox

	// allowlistHandler is invoked when the user picks option 2 ("同意并不再询问").
	allowlistHandler func(toolName string, params map[string]interface{})

	// newSessionHandler is invoked when the user requests a brand-new session
	// (via Ctrl+L or /clear). The handler is expected to create a new agent
	// session and return its ID for status display.
	newSessionHandler func() string

	// Token status (令牌细分展示)
	tokenStatus string

	showingCommands      bool
	commands             []slash.Command
	commandsIndex        int
	commandsScrollOffset int // first visible row index in the commands palette
	commandItems         []commandHitBox
	// slashNames caches the slash-prefixed command names for Tab completion.
	// Refreshed whenever the commands palette is opened via loadCommands().
	slashNames []string

	// Permission management
	permissionManager *permission.Manager
	permissionMode    string // cached permission mode for status bar display

	// Persistent allowlist for /disallow cleanup
	persistentAllowlist *permission.PersistentAllowlistStore
	workdir             string

	// Engine management
	engine *engine.Engine

	// Available tool names for Tab completion
	availableToolNames []string

	// Streaming text aggregation
	streamingBuf    strings.Builder
	isStreaming     bool
	lastStreamFlush time.Time

	// Thinking block (collapsible reasoning preview)
	thinkingTitle     string
	thinkingReasoning string
	thinkingCollapsed bool
	thinkingCompleted bool
	thinkingWindow    []string
	thinkingPending   string

	// Input history
	inputHistory  []string
	historyIndex  int    // -1 means "not browsing history"
	historyDraft  string // draft saved when user starts browsing history
	historySearch *HistorySearch

	// showHelp controls whether the full help line is visible. Toggled by
	// pressing `?`; starts collapsed so the input area gets maximum space.
	showHelp bool

	// Content display phase management
	currentPhase        displayPhase
	altScreenActive     bool
	historyScrollOffset int
	historySelected     int

	// messageScrollOffsetLines tracks how many rendered lines the inline
	// message-window is scrolled up from the bottom. 0 anchors to the
	// latest content; positive values reveal older rows. Modified by
	// PgUp/PgDn and mouse-wheel handlers and reset whenever the user
	// submits a new prompt.
	messageScrollOffsetLines int

	// Tool call tracking for deduplication
	activeToolCalls  map[string]string // ID -> last displayed status
	connectionState  string
	connectionDetail string
	notice           string
	swarmLine        string

	// teamName, if non-empty, scopes /teammates and /agents listings.
	teamName string

	// Spinner and context status
	spinnerFrame      int
	spinnerStage      string
	spinnerVerbs      []string // Effective spinner verbs from config
	selectedVerb      string   // verb selected at start of thinking cycle
	contextWindowMax  int
	contextUsedTokens int

	// # file picker
	showingFilePicker bool
	filePickerQuery   string
	filePickerResults []string
	filePickerCursor  int
	fileIndex         *filesearch.Index

	// termCap describes which characters the host terminal can render.
	// Populated in New() and re-evaluated on resize; used by safe-char
	// rendering helpers to avoid garbled output on constrained terminals.
	termCap TermCapability

	// localDispatcher handles slash commands that can be answered locally
	// without contacting the backend agent (e.g. /agents, /checkpoint,
	// /models). It is constructed lazily on first use so unit tests that
	// do not exercise slash commands stay decoupled from the slash package.
	localDispatcher *slash.LocalDispatcher

	// attachmentMgr manages files saved to .nano/attachments/
	attachmentMgr *attachment.Manager
	// pendingImages tracks images queued for the next user message
	pendingImages []llm.MultimodalImage
	// imageIndicator shows a UI indicator when images are attached
	imageIndicator string

	modelLister          func() string
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
}

type hitBox struct {
	// x1 == 0 means the row is intentionally unbounded horizontally.
	x0, x1 int
	y      int
}

type commandHitBox struct {
	hitBox
	index int
}

func New(submitCh chan<- string, cancelCh chan<- struct{}, apiBaseURL, cwd string) *Model { //nolint:revive
	ta := textarea.New()
	ta.Placeholder = "输入您的请求...  (Ctrl+J 换行 | Enter 发送 | 行尾 \\ 续行)"
	ta.Focus()
	ta.CharLimit = 10000 // Generous limit to accommodate long inputs
	ta.SetWidth(50)
	ta.SetHeight(minInputHeight)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.KeyMap.InsertNewline.SetEnabled(false)
	applyTextareaTheme(&ta)

	fileIndex, err := filesearch.NewIndex(cwd)
	if err == nil {
		if crawlErr := fileIndex.Crawl(); crawlErr != nil {
			logger.Warnf("Failed to crawl file index: %v", crawlErr)
		}
	} else {
		logger.Warnf("Failed to initialize file index: %v", err)
	}

	return &Model{
		SubmitCh:        submitCh,
		CancelCh:        cancelCh,
		messages:        NewMessageStore(),
		status:          "等待输入",
		apiBaseURL:      apiBaseURL,
		cwd:             cwd,
		input:           ta,
		terminalFocused: true,
		termCap:         DetectTermCapability(),

		// History buffer initialization
		sessionStartTime: time.Now(),

		thinkingCollapsed: true,

		historyIndex:  -1,
		historySearch: NewHistorySearch(nil),

		// Initialize phase and tool tracking
		currentPhase:    phaseIdle,
		activeToolCalls: make(map[string]string),
		fileIndex:       fileIndex,
		spinnerVerbs:    spinnerverbs.EffectiveVerbs(nil), // Initialize with default verbs
	}
}

func (m *Model) Init() tea.Cmd { //nolint:revive
	return tea.Batch(textarea.Blink, spinnerTickCmd())
}

func (m *Model) BindOutbound(send func(eventsource.Outbound) error) {
	m.outbound = send
}

func (m *Model) SendOutbound(o eventsource.Outbound) error {
	if m.outbound == nil {
		return nil
	}
	return m.outbound(o)
}

// SetSpinnerVerbsConfig updates the spinner verbs configuration.
func (m *Model) SetSpinnerVerbsConfig(cfg *config.SpinnerVerbsConfig) {
	m.spinnerVerbs = spinnerverbs.EffectiveVerbs(cfg)
}

// ThinkingWindow returns a snapshot of the current 3-line rolling thinking buffer.
// The returned slice is a copy.
func (m *Model) ThinkingWindow() []string {
	out := make([]string, len(m.thinkingWindow))
	copy(out, m.thinkingWindow)
	return out
}

// SpinnerStage returns the current spinner stage label.
func (m *Model) SpinnerStage() string {
	return m.spinnerStage
}

// SpinnerFrameIndex returns the internal spinner frame counter.
func (m *Model) SpinnerFrameIndex() int {
	return m.spinnerFrame
}

// SelectedVerb returns the verb selected for the current thinking cycle.
func (m *Model) SelectedVerb() string {
	return m.selectedVerb
}

// ContextBarState returns the context bar token state and clamped percentage.
func (m *Model) ContextBarState() (int, int, float64) {
	if m.contextWindowMax <= 0 {
		return m.contextUsedTokens, m.contextWindowMax, 0
	}
	pct := float64(m.contextUsedTokens) / float64(m.contextWindowMax)
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	return m.contextUsedTokens, m.contextWindowMax, pct
}

// FilePickerState returns the current file picker UI state.
func (m *Model) FilePickerState() (active bool, query string, cursor int, results []string) {
	results = make([]string, len(m.filePickerResults))
	copy(results, m.filePickerResults)
	return m.showingFilePicker, m.filePickerQuery, m.filePickerCursor, results
}

// ConfirmationState returns the current confirmation dialog state.
func (m *Model) ConfirmationState() (showing bool, selected int) {
	return m.showingConfirmation, m.confirmationSelected
}

// InputValue returns the current textarea value.
func (m *Model) InputValue() string {
	return m.input.Value()
}

// ClearSession resets all UI state and (if handler set) starts a new agent session.
// Returns the new session ID if one was created.
func (m *Model) ClearSession() string {
	// Reset all session state. m.messages is always initialised by NewModel
	// (and by test helpers), so no nil guard is needed here.
	m.messages = NewMessageStore()
	m.sessionStartTime = time.Now()
	m.resetThinkingState()
	m.streamingBuf.Reset()
	m.isStreaming = false
	m.lastStreamFlush = time.Time{}
	m.altScreenActive = false
	m.historyScrollOffset = 0
	m.showingConfirmation = false
	m.confirmationMessage = ""
	m.confirmationToolInfo = nil
	m.confirmationCallback = nil
	m.confirmationAlwaysCallback = nil
	m.confirmationSelected = 0
	m.confirmationIsPaste = false
	m.pendingPaste = ""
	m.confirmationButtons = nil
	m.showingFilePicker = false
	m.filePickerQuery = ""
	m.filePickerResults = nil
	m.filePickerCursor = 0
	m.activeToolCalls = make(map[string]string)
	m.resetSpinnerToIdle()
	m.swarmLine = ""
	m.notice = ""
	m.contextUsedTokens = 0
	m.contextWindowMax = 0
	m.currentPhase = phaseIdle
	m.tokenStatus = ""
	m.status = m.formatStatusForPhase(phaseIdle, "")
	m.input.Reset()
	m.adjustInputHeight()

	newID := ""
	if m.newSessionHandler != nil {
		newID = m.newSessionHandler()
	}
	return newID
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:revive
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.altScreenActive {
			return m, m.handleFullscreenHistoryKey(msg)
		}
		if m.showingCommands {
			switch msg.String() {
			case "up", "k":
				if m.commandsIndex > 0 {
					m.commandsIndex--
					// Sticky scroll: scroll up when selection approaches top edge
					if m.commandsIndex-m.commandsScrollOffset < commandsPaletteScrollPadding {
						m.commandsScrollOffset = m.commandsIndex - commandsPaletteScrollPadding
						if m.commandsScrollOffset < 0 {
							m.commandsScrollOffset = 0
						}
					}
				}
				return m, nil
			case "down", "j":
				if m.commandsIndex < len(m.commands)-1 {
					m.commandsIndex++
					maxOffset := len(m.commands) - commandsPaletteVisibleRows
					if maxOffset < 0 {
						maxOffset = 0
					}
					// Sticky scroll: scroll down when selection approaches bottom edge
					if m.commandsIndex-m.commandsScrollOffset >= commandsPaletteVisibleRows-commandsPaletteScrollPadding {
						m.commandsScrollOffset = m.commandsIndex - commandsPaletteVisibleRows + commandsPaletteScrollPadding + 1
						if m.commandsScrollOffset > maxOffset {
							m.commandsScrollOffset = maxOffset
						}
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
				maxSelection := 2
				if m.confirmationIsPaste {
					maxSelection = 1
				}
				if m.confirmationSelected < maxSelection {
					m.confirmationSelected++
				}
				return m, nil
			case "enter":
				if m.confirmationIsPaste {
					if m.confirmationSelected != 1 {
						m.insertInputText(m.pendingPaste)
					}
					m.hideConfirmation()
					return m, nil
				}
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

		if m.historySearch != nil && m.historySearch.Active() {
			if handled, cmd := m.handleHistorySearchKey(msg); handled {
				return m, cmd
			}
		}

		if m.showingFilePicker {
			switch msg.String() {
			case "up":
				if m.filePickerCursor > 0 {
					m.filePickerCursor--
				}
				return m, nil
			case "down":
				if m.filePickerCursor < len(m.filePickerResults)-1 {
					m.filePickerCursor++
				}
				return m, nil
			case "enter":
				m.insertSelectedFile()
				m.showingFilePicker = false
				return m, nil
			case "esc":
				m.showingFilePicker = false
				return m, nil
			}
		}

		// Normal input handling when not showing confirmation
		switch msg.String() {
		case "pgup":
			// Scroll the inline message window up by ~half a viewport
			// so the user can review earlier conversation history.
			vp := m.inlineMessageViewportLines()
			step := vp / 2
			if step < 1 {
				step = 1
			}
			m.messageScrollOffsetLines += step
			m.clampMessageScrollOffset(vp)
			return m, nil
		case "pgdown":
			vp := m.inlineMessageViewportLines()
			step := vp / 2
			if step < 1 {
				step = 1
			}
			m.messageScrollOffsetLines -= step
			m.clampMessageScrollOffset(vp)
			return m, nil
		case "ctrl+c":
			if m.outbound != nil {
				// Adapter/EventSource mode: cancel if a turn is in progress, quit if idle.
				// Note: isActivePhase() covers phaseThinking/phaseToolCall/phaseAwaitingApproval;
				// phaseProcessing and phaseResponse must be checked explicitly since they are
				// not returned by isActivePhase().
				if m.currentPhase == phaseProcessing || m.currentPhase == phaseResponse ||
					m.isActivePhase() || m.isStreaming {
					return m, m.outboundCmd(eventsource.Outbound{Kind: "cancel"})
				}
				if dump := m.dumpHistory(); dump != "" {
					return m, tea.Sequence(tea.Printf("%s", dump), tea.Quit)
				}
				return m, tea.Quit
			}
			if dump := m.dumpHistory(); dump != "" {
				return m, tea.Sequence(tea.Printf("%s", dump), tea.Quit)
			}
			return m, tea.Quit
		case "ctrl+z":
			return m, m.outboundCmd(eventsource.Outbound{Kind: "cancel"})
		case "up":
			if m.input.Line() > 0 {
				break
			}
			// Browse input history (most-recent first)
			if len(m.inputHistory) == 0 {
				break
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
			if m.input.Line() < m.input.LineCount()-1 {
				break
			}
			if m.historyIndex == -1 {
				break
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
		case "shift+tab":
			// M3-2: cycle through permission modes when a permission manager
			// is wired. Order: default → acceptEdits → plan → auto → yolo → default.
			if m.permissionManager != nil {
				next := cyclePermissionMode(m.permissionMode)
				m.permissionManager.SetMode(next)
				m.permissionMode = string(next)
				notice := fmt.Sprintf("🔁 权限模式 → %s %s", m.permissionModeIcon(string(next)), next)
				m.recordMessage("system", notice)
				return m, nil
			}
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
					hintContent := "补全候选：" + strings.Join(suggestions, "  ")
					m.recordMessage("system", hintContent)
					return m, nil
				}
			}
			return m, nil
		case "enter":
			if msg.Code == tea.KeyEnter && msg.Mod&tea.ModShift != 0 {
				m.insertInputNewline()
				return m, nil
			}
			val := m.input.Value()
			if strings.HasSuffix(val, "\\") {
				m.input.SetValue(strings.TrimSuffix(val, "\\") + "\n")
				m.input.CursorEnd()
				m.adjustInputHeight()
				return m, nil
			}
			if strings.TrimSpace(val) != "" {
				input := val

				// Intercept permission slash commands before forwarding to agent
				if strings.HasPrefix(input, "/") {
					if m.shouldForwardBackendControl(input) {
						m.input.Reset()
						m.historyIndex = -1
						return m, m.outboundCmd(eventsource.Outbound{Kind: "control", Control: strings.TrimSpace(input)})
					}
					if handled, cmd := m.handlePermissionSlashCommand(input); handled {
						m.input.Reset()
						m.historyIndex = -1
						return m, cmd
					}
					dispatched := m.getLocalDispatcher().Dispatch(input)
					if dispatched.Handled {
						_, cmd := m.recordLocalSlashResult(dispatched)
						m.input.Reset()
						m.historyIndex = -1
						return m, cmd
					}
					if dispatched.ShouldSubmit {
						// Show the original "/cmd ..." in the transcript but
						// submit the dispatcher-rewritten text to the agent.
						m.recordMessage("user", input)
						submit := dispatched.SubmitInput
						cmds = append(cmds, m.outboundCmd(eventsource.Outbound{Kind: "submit", Text: submit}))
						if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != input {
							m.inputHistory = append(m.inputHistory, input)
							if len(m.inputHistory) > 100 {
								m.inputHistory = m.inputHistory[1:]
							}
						}
						m.historyIndex = -1
						m.historyDraft = ""
						m.currentPhase = phaseProcessing
						m.status = m.formatStatusForPhase(phaseProcessing, "")
						m.input.Reset()
						m.showingFilePicker = false
						m.messageScrollOffsetLines = 0
						return m, tea.Batch(cmds...)
					}
				}

				m.recordMessage("user", input)

				// Parse @file references for images and add to pending images
				if m.attachmentMgr != nil {
					fileRefs := attachment.ParseFileReference(input)
					for _, ref := range fileRefs {
						if attachment.IsImageFile(ref) {
							img, err := m.attachmentMgr.ToMultimodalImage(ref)
							if err == nil {
								m.pendingImages = append(m.pendingImages, img)
							} else {
								logger.Warnf("Failed to load image from @file reference %s: %v", ref, err)
							}
						}
					}
				}

				// Expand `@file[:start-end]` references before forwarding so
				// the agent sees the actual content. The original token is
				// preserved as a header in the expansion so the user-visible
				// transcript still shows what was referenced.
				expanded := ExpandFileReferences(input, m.cwd)

				// If we have pending images, submit via multimodal outbound
				if len(m.pendingImages) > 0 {
					cmds = append(cmds, m.outboundCmd(eventsource.Outbound{
						Kind:   "submit",
						Text:   expanded,
						Images: m.pendingImages,
					}))
					// Clear pending images after submission
					m.pendingImages = nil
					m.imageIndicator = ""
				} else {
					cmds = append(cmds, m.outboundCmd(eventsource.Outbound{Kind: "submit", Text: expanded}))
				}

				// Record in history (avoid consecutive duplicates), cap at 100 entries
				if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != input {
					m.inputHistory = append(m.inputHistory, input)
					if len(m.inputHistory) > 100 {
						m.inputHistory = m.inputHistory[1:]
					}
				}
				m.historyIndex = -1
				m.historyDraft = ""

				m.currentPhase = phaseProcessing
				m.status = m.formatStatusForPhase(phaseProcessing, "")
				m.input.Reset()
				m.showingFilePicker = false
				// Anchor the message window to the bottom whenever a
				// new prompt is submitted so the user sees the latest
				// turn without having to scroll back manually.
				m.messageScrollOffsetLines = 0
			}
		case "ctrl+p":
			m.openCommandsPalette()
			return m, nil
		case "ctrl+r":
			// Ctrl+R: reverse history search.
			m.startHistorySearch()
			return m, nil
		case "ctrl+f":
			m.enterFullscreenHistory()
			return m, nil
		case "ctrl+t":
			if m.thinkingReasoning != "" {
				m.thinkingCollapsed = !m.thinkingCollapsed
			}
			return m, nil
		case "ctrl+y":
			if last := m.lastAssistantReply(); last != "" {
				return m, tea.SetClipboard(last)
			}
			return m, nil
		case "ctrl+l":
			// Ctrl+L: Start a new session (clear context) - same as /clear
			return m, m.clearSessionCmd()
		case "ctrl+j", "shift+enter":
			m.insertInputNewline()
			return m, nil
		case "?", "shift+/":
			if strings.TrimSpace(m.input.Value()) == "" {
				m.showHelp = !m.showHelp
				return m, nil
			}
		}

	case tea.KeyboardEnhancementsMsg:
		m.keyboardEnhanced = msg.SupportsKeyDisambiguation()
		return m, nil

	case tea.PasteMsg:
		// Before processing as text, check if the clipboard contains an image
		if m.attachmentMgr != nil {
			contentType := clipboard.DetectContentType()
			if contentType == clipboard.ContentImage {
				return m, m.handleImagePaste()
			}
			if contentType == clipboard.ContentFilePath {
				return m, m.handleFilePaste()
			}
		}

		// Normal text paste handling
		if m.isLargePaste(msg.Content) {
			m.showPasteConfirmation(msg.Content)
			return m, nil
		}
		m.insertInputText(msg.Content)
		return m, nil

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		if m.showingConfirmation {
			if btn := m.hitTestConfirmationButton(mouse.X, mouse.Y); btn >= 0 {
				m.confirmationSelected = btn
				if m.confirmationIsPaste {
					if m.confirmationSelected != 1 {
						m.insertInputText(m.pendingPaste)
					}
					m.hideConfirmation()
					return m, nil
				}
				cmd := m.buildConfirmationCmd()
				m.hideConfirmation()
				return m, cmd
			}
			return m, nil
		}
		if m.showingCommands {
			if idx := m.hitTestCommandItem(mouse.X, mouse.Y); idx >= 0 {
				m.commandsIndex = idx
				if len(m.commands) > 0 {
					name := m.commands[m.commandsIndex].Name
					m.input.SetValue("/" + name + " ")
				}
				m.showingCommands = false
				return m, nil
			}
			return m, nil
		}

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if m.altScreenActive {
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.scrollFullscreenHistory(-historyWheelScrollLines)
			case tea.MouseWheelDown:
				m.scrollFullscreenHistory(historyWheelScrollLines)
			}
			return m, nil
		}
		if m.showingCommands {
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.moveCommandSelection(-1)
			case tea.MouseWheelDown:
				m.moveCommandSelection(1)
			}
			return m, nil
		}
		// Inline message-window scroll.
		vp := m.inlineMessageViewportLines()
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.messageScrollOffsetLines += historyWheelScrollLines
		case tea.MouseWheelDown:
			m.messageScrollOffsetLines -= historyWheelScrollLines
		}
		m.clampMessageScrollOffset(vp)
		return m, nil

	case tea.FocusMsg:
		m.terminalFocused = true
		if m.shouldRunSpinner() {
			return m, spinnerTickCmd()
		}
		return m, nil

	case tea.BlurMsg:
		m.terminalFocused = false
		return m, nil

	case tea.WindowSizeMsg:
		if msg.Width == m.termWidth && msg.Height == m.termHeight {
			return m, nil
		}

		m.termWidth = msg.Width
		m.termHeight = msg.Height
		inputContentWidth := msg.Width - inputContentMargin
		if inputContentWidth > 0 {
			// textarea.SetWidth accepts outer width, so account for the
			// focused border frame while preserving the content margin.
			m.input.SetWidth(inputContentWidth + m.input.Styles().Focused.Base.GetHorizontalFrameSize())
		}
		m.adjustInputHeight()

		// Invalidate cached rendering so the message window re-wraps
		// lines at the new terminal width.
		m.messages.InvalidateCache()
		m.clampMessageScrollOffset(m.inlineMessageViewportLines())

		// Clear once after resize to prevent stale rows from prior renders.
		return m, tea.ClearScreen

	case SpinnerTickMsg:
		if !m.shouldRunSpinner() {
			return m, nil
		}
		if m.shouldAnimateSpinner() {
			m.spinnerFrame++
		}
		return m, spinnerTickCmd()

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

		// Reset only when fresh full reasoning or a reasoning delta starts a new block.
		// Bare title/status thinking updates between tool calls preserve the last preview.
		if isNewSession && (msg.Reasoning != "" || msg.ReasoningDelta != "") {
			m.thinkingReasoning = ""
			m.thinkingPending = ""
			m.thinkingWindow = nil
		}
		if msg.Reasoning != "" {
			m.thinkingReasoning = msg.Reasoning
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
			// Only persist a thinking message when there is actual reasoning
			// content. Empty complete events should not pollute the
			// scrollback with a "思考完成 [0 字符]" placeholder.
			if strings.TrimSpace(m.thinkingReasoning) != "" {
				storeContent := summaryContent + "\n" + m.thinkingReasoning
				m.recordMessage("thinking", storeContent)
				// Keep a single-line preview visible in the status area so the
				// user can confirm the thinking finished even after the
				// assistant stream begins. The preview is non-destructive: the
				// full reasoning lives in the MessageStore and can be revealed
				// with Ctrl+T.
				m.thinkingWindow = []string{summaryContent + "，Ctrl+T 展开"}
			} else {
				m.thinkingWindow = nil
			}
			m.thinkingPending = ""
		} else {
			m.setAgentStatus(phaseThinking, "")
		}
		return m, tea.Batch(cmds...)

	case StatusUpdate:
		// Flush any in-progress streaming buffer before changing status
		if m.isStreaming {
			cmds = append(cmds, m.flushStreamingBuffer()...)
		}
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
		return m, tea.Batch(cmds...)

	case CronStatusMsg:
		m.cronIndicator = msg.Indicator
		return m, nil

	case ToolUseMsg:
		m.advanceSpinner("executing")
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
				content := fmt.Sprintf("正在调用工具: %s", msg.ToolName)
				m.recordMessage("tool", content)
			}
			// Otherwise skip to avoid duplicate "executing" messages
		case "success":
			if lastStatus != "success" {
				// Print completion marker (concise version); normalize whitespace in preview
				resultPreview := truncateResult(strings.ReplaceAll(msg.Result, "\n", " "), 200)
				content := fmt.Sprintf("工具: %s 调用完成\n结果: %s", msg.ToolName, resultPreview)
				m.recordMessage("tool", content)
			}
		case "error":
			// Always print errors
			content := fmt.Sprintf("工具 %s 执行失败: %s", msg.ToolName, msg.Result)
			m.recordMessage("error", content)
		case "cancelled":
			// Print cancellation notice unless already shown
			if lastStatus != "cancelled" {
				content := fmt.Sprintf("工具 %s 已取消", msg.ToolName)
				m.recordMessage("tool", content)
			}
		}

		// Clean up terminal-state entries to avoid unbounded memory growth
		switch msg.Status {
		case "success", "error", "cancelled":
			delete(m.activeToolCalls, msg.ID)
		default:
			m.activeToolCalls[msg.ID] = msg.Status
		}

		// Update status bar to reflect tool execution state
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

	case Message:
		m.advanceSpinner("writing")
		if msg.Role == "assistant_stream" {
			// Accumulate streaming chunks and flush incrementally so long replies
			// appear progressively instead of only at task completion.
			if !m.isStreaming {
				m.isStreaming = true
				m.streamingBuf.Reset()
				m.lastStreamFlush = time.Now()
			}
			m.streamingBuf.WriteString(msg.Content)
			if strings.Contains(msg.Content, "\n") || m.shouldFlushPartial() {
				cmds = append(cmds, m.flushStreamingIncremental()...)
				return m, tea.Batch(cmds...)
			}
			return m, nil
		}

		// Non-streaming message: flush any pending stream first
		if m.isStreaming {
			cmds = append(cmds, m.flushStreamingBuffer()...)
		}

		m.recordMessage(msg.Role, msg.Content)
		if msg.Role == "assistant" || msg.Role == "assistant_stream" {
			m.setAgentStatus(phaseResponse, "")
		}

	case ShowConfirmationMsg:
		m.showingCommands = false // dismiss commands palette so dialog is always visible
		m.showingConfirmation = true
		m.confirmationMessage = msg.Message
		m.confirmationToolInfo = msg.ToolInfo
		m.confirmationCallback = msg.Callback
		m.confirmationAlwaysCallback = msg.AlwaysCallback
		m.confirmationSelected = 0
		toolName, _ := msg.ToolInfo["Name"].(string)
		m.setAgentStatus(phaseAwaitingApproval, toolName)
		return m, nil

	case TaskCompletionMsg:
		if m.isStreaming {
			cmds = append(cmds, m.flushStreamingBuffer()...)
		}
		m.resetThinkingState()
		m.activeToolCalls = make(map[string]string)
		m.resetSpinnerToIdle()
		m.currentPhase = phaseDone
		m.status = m.formatStatusForPhase(phaseDone, "")
		return m, tea.Batch(cmds...)

	case TokenStatsUpdate: // Handle token stats updates
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
			tokens += fmt.Sprintf(" | 上下文: %s/%s (%.0f%%)",
				formatCount(effUsed),
				formatCount(effMax), pct)
		}
		m.tokenStatus = tokens
		if msg.ContextWindowMax > 0 {
			m.contextWindowMax = msg.ContextWindowMax
		}
		if msg.ContextUsedTokens > 0 {
			m.contextUsedTokens = msg.ContextUsedTokens
		}
		return m, nil
	// The following cases are fed by BubbleTeaAdapter (nano chat / nano
	// lead-chat paths).  The runBubbleTeaMode (--tea / --milktea) path does
	// not currently emit these message types through forwardBubbleTeaStreamEvent;
	// they are kept here so the BubbleTeaAdapter route continues to work.
	case ConnectionStatusMsg: // BubbleTeaAdapter (nano chat) path only
		m.connectionState = msg.State
		m.connectionDetail = msg.Detail
		return m, nil
	case NoticeMsg: // BubbleTeaAdapter (nano chat) path only
		m.notice = string(msg)
		m.recordMessage("system", string(msg))
		return m, nil
	case MailboxMsg: // BubbleTeaAdapter (nano chat) path only
		m.swarmLine = fmt.Sprintf("📬 %s → %s [%s] %s", msg.From, msg.To, msg.Kind, msg.Preview)
		return m, nil
	case IdleNotifyMsg: // BubbleTeaAdapter (nano chat) path only
		m.swarmLine = fmt.Sprintf("💤 %s: %s", msg.Agent, msg.Summary)
		return m, nil
	case SpawnTeammateMsg: // BubbleTeaAdapter (nano chat) path only
		m.swarmLine = fmt.Sprintf("🧑‍💻 %s spawned for %s (%s)", msg.Agent, msg.Topic, msg.SessionID)
		return m, nil

	case ImagePasteSuccessMsg:
		m.pendingImages = append(m.pendingImages, msg.Image)
		m.updateImageIndicator()
		return m, nil

	case ImagePasteErrorMsg:
		logger.Warnf("Image paste failed: %v", msg.Error)
		m.recordMessage("system", fmt.Sprintf("⚠️  Image paste failed: %v", msg.Error))
		return m, nil
	}

	// Update input
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.adjustInputHeight()
	m.updateFilePickerState()
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) outboundCmd(o eventsource.Outbound) tea.Cmd {
	if m.outbound != nil {
		send := m.outbound
		return func() tea.Msg {
			_ = send(o)
			return nil
		}
	}
	switch o.Kind {
	case "submit":
		if m.SubmitCh != nil {
			return func() tea.Msg {
				m.SubmitCh <- o.Text
				return nil
			}
		}
	case "cancel":
		if m.CancelCh != nil {
			return func() tea.Msg {
				select {
				case m.CancelCh <- struct{}{}:
				default:
				}
				return nil
			}
		}
	}
	return nil
}

func (m *Model) insertInputNewline() {
	m.insertInputText("\n")
}

func (m *Model) insertInputText(s string) {
	m.input.InsertString(s)
	m.adjustInputHeight()
}

func (m *Model) isLargePaste(content string) bool {
	if len(content) > largePasteBytes {
		return true
	}
	return strings.Count(content, "\n") > largePasteLines
}

func (m *Model) showPasteConfirmation(content string) {
	lineCount := strings.Count(content, "\n") + 1
	m.showingCommands = false
	m.showingConfirmation = true
	m.confirmationMessage = fmt.Sprintf("检测到大段内容（%d 字节，%d 行），是否插入到输入框？", len(content), lineCount)
	m.confirmationToolInfo = map[string]interface{}{
		"Name": "paste",
		"Parameters": map[string]interface{}{
			"bytes": len(content),
			"lines": lineCount,
		},
	}
	m.confirmationCallback = nil
	m.confirmationSelected = 1
	m.confirmationIsPaste = true
	m.pendingPaste = content
	m.setAgentStatus(phaseAwaitingApproval, "粘贴确认")
}

func (m *Model) startHistorySearch() {
	if m.historySearch == nil {
		m.historySearch = NewHistorySearch(m.inputHistory)
	}
	if !m.historySearch.Active() {
		m.historyDraft = m.input.Value()
		m.historySearch = NewHistorySearch(m.inputHistory)
		m.historySearch.Begin()
		m.status = "历史搜索：输入关键词，Ctrl+R 查找更早记录，Enter 接受，Esc 取消"
		return
	}
	m.historySearch.Next()
	m.applyHistorySearchSelection()
}

func (m *Model) handleHistorySearchKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "ctrl+r":
		m.historySearch.Next()
		m.applyHistorySearchSelection()
		return true, nil
	case "enter":
		m.historySearch.End()
		m.historyIndex = -1
		m.status = m.formatStatusForPhase(m.currentPhase, "")
		return true, nil
	case "esc", "ctrl+c":
		m.input.SetValue(m.historyDraft)
		m.input.CursorEnd()
		m.historySearch.End()
		m.historyIndex = -1
		m.status = m.formatStatusForPhase(m.currentPhase, "")
		return true, nil
	case "backspace":
		m.historySearch.Backspace()
		m.applyHistorySearchSelection()
		return true, nil
	}
	if msg.Text != "" {
		for _, r := range msg.Text {
			m.historySearch.TypeRune(r)
		}
		m.applyHistorySearchSelection()
		return true, nil
	}
	return true, nil
}

func (m *Model) applyHistorySearchSelection() {
	query := m.historySearch.Query()
	selected := m.historySearch.Selected()
	if selected != "" {
		m.input.SetValue(selected)
		m.input.CursorEnd()
		m.status = fmt.Sprintf("历史搜索：%q → %s", query, truncateHistoryPreview(selected))
		return
	}
	if query == "" {
		m.input.SetValue(m.historyDraft)
		m.input.CursorEnd()
		m.status = "历史搜索：输入关键词，Ctrl+R 查找更早记录，Enter 接受，Esc 取消"
		return
	}
	m.status = fmt.Sprintf("历史搜索：%q 无匹配", query)
}

func truncateHistoryPreview(s string) string {
	s = strings.ReplaceAll(s, "\n", `\n`)
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// inlineMessageViewportLines computes the height of the inline message
// window in terminal rows using the same accounting as View(). The
// PgUp/PgDn and mouse-wheel handlers use it to step ~half a viewport
// and to clamp messageScrollOffsetLines.
func (m *Model) inlineMessageViewportLines() int {
	if m == nil || m.termHeight <= 0 {
		return 0
	}
	var inputBuf strings.Builder
	m.renderInputSection(&inputBuf)
	inputLines := renderedLineCount(inputBuf.String())
	return m.termHeight - inputLines
}

func (m *Model) adjustInputHeight() {
	n := m.input.LineCount()
	if n < minInputHeight {
		n = minInputHeight
	}
	if n > maxInputHeight {
		n = maxInputHeight
	}
	if m.input.Height() != n {
		m.input.SetHeight(n)
	}
}

// clearSessionCmd clears the agent session and prints feedback. The cursed
// renderer in Bubble Tea v2 handles redraw during resizes natively, so there
// is no application-level rewrap helper any longer.
func (m *Model) clearSessionCmd() tea.Cmd {
	newID := m.ClearSession()
	msg := "已开启新会话"
	if newID != "" {
		msg = fmt.Sprintf("已开启新会话 (id: %s)", newID)
	}
	m.recordMessage("system", msg)

	cmds := []tea.Cmd{
		tea.ClearScreen,
	}
	if m.outbound != nil {
		cmds = append(cmds, m.outboundCmd(eventsource.Outbound{Kind: "control", Control: "/clear"}))
	}
	return tea.Sequence(cmds...)
}

func (m *Model) shouldForwardBackendControl(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "/reset", "/sessions", "/cancel":
		return m.outbound != nil
	default:
		return false
	}
}

func (m *Model) setAgentStatus(phase displayPhase, detail string) {
	if (phase == phaseIdle || phase == phaseDone) && m.isActivePhase() {
		return
	}
	m.currentPhase = phase
	m.status = m.formatStatusForPhase(phase, detail)
}

func (m *Model) transitionToDonePhase() {
	m.currentPhase = phaseDone
	m.status = m.formatStatusForPhase(phaseDone, "")
}

func (m *Model) formatStatusForPhase(phase displayPhase, detail string) string {
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
		return "✅ 完成"
	default:
		return ""
	}
}

func (m *Model) isActivePhase() bool {
	switch m.currentPhase {
	case phaseThinking, phaseToolCall, phaseAwaitingApproval:
		return true
	default:
		return false
	}
}

// buildConfirmationCmd captures the current confirmation state and returns a
// tea.Cmd that executes the user's choice in a background goroutine.
// This avoids calling the callback synchronously from Update(), which would
// trigger p.Send() while the event loop is still processing – causing a deadlock.
func (m *Model) buildConfirmationCmd() tea.Cmd {
	callback := m.confirmationCallback
	alwaysCallback := m.confirmationAlwaysCallback
	selected := m.confirmationSelected
	toolInfo := m.confirmationToolInfo
	ah := m.allowlistHandler
	isPaste := m.confirmationIsPaste

	return func() tea.Msg {
		if isPaste {
			if selected != 1 && callback != nil {
				callback(true)
			}
			return nil
		}
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
			// If an AlwaysCallback is set (e.g. by BubbleTeaAdapter to send
			// Always:true to the daemon), call it exclusively so we don't also
			// send a plain Allow:true approval via the regular callback.
			if alwaysCallback != nil {
				alwaysCallback()
			} else if callback != nil {
				callback(true)
			}
			if ah != nil {
				ah(toolName, paramsCopy)
			}
		}
		return nil
	}
}

func (m *Model) shouldFlushPartial() bool {
	return m.streamingBuf.Len() > 0 && time.Since(m.lastStreamFlush) >= streamFlushInterval
}

// flushStreamingIncremental updates the streaming message in the store.
// The message window in View() will pick up the updated content on the
// next render — no tea.Printf is needed.
func (m *Model) flushStreamingIncremental() []tea.Cmd {
	if !m.isStreaming || m.streamingBuf.Len() == 0 {
		return nil
	}
	chunk := m.streamingBuf.String()
	m.streamingBuf.Reset()
	m.lastStreamFlush = time.Now()

	m.appendStreamingChunkToLine(chunk)
	m.setAgentStatus(phaseResponse, "")
	return nil
}

// flushStreamingBuffer emits any remaining streaming content and terminates the
// streaming state.
func (m *Model) flushStreamingBuffer() []tea.Cmd {
	if !m.isStreaming || m.streamingBuf.Len() == 0 {
		m.isStreaming = false
		return nil
	}
	chunk := m.streamingBuf.String()
	m.streamingBuf.Reset()
	m.isStreaming = false
	m.lastStreamFlush = time.Now()

	if chunk != "" {
		m.appendStreamingChunkToLine(chunk)
	}
	m.setAgentStatus(phaseResponse, "")
	return nil
}

// appendStreamingChunkToLine appends chunk to the active assistant_stream
// entry in the MessageStore so that View() renders the latest partial
// response on every tick. The MessageStore entry is the canonical
// accumulator — no secondary buffer is kept. A new entry is created on the
// first chunk of each streaming session; subsequent chunks append in place.
func (m *Model) appendStreamingChunkToLine(chunk string) {
	// Append to the existing streaming entry when one is active.
	if last := m.messages.Last(); last != nil && last.Role == "assistant_stream" {
		last.Content += chunk
		last.InvalidateCache()
		return
	}
	// First chunk: create a new streaming message in the store.
	m.recordMessage("assistant_stream", chunk)
}

func (m *Model) appendThinkingDelta(delta string) {
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
	displayLines := make([]string, len(lines))
	for i := range lines {
		displayLines[i] = strings.TrimSpace(lines[i])
	}

	if len(displayLines) > 3 {
		m.thinkingWindow = displayLines[len(displayLines)-3:]
	} else {
		m.thinkingWindow = displayLines
	}
	if n := len(lines); n > 0 {
		m.thinkingPending = lines[n-1]
		// wordwrap trims a trailing delimiter from the last line; keep it so
		// the next delta preserves word boundaries during incremental wrapping.
		if strings.HasSuffix(rawPending, " ") && !strings.HasSuffix(m.thinkingPending, " ") {
			m.thinkingPending += " "
		}
	}
}

func (m *Model) updateThinkingWindow(preview string) {
	normalized := strings.TrimSpace(strings.ReplaceAll(preview, "\n", " "))
	if normalized == "" {
		return
	}
	width := m.thinkingWrapWidth()
	lines := strings.Split(wordwrap.String(normalized, width), "\n")
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

func (m *Model) thinkingWrapWidth() int {
	width := m.termWidth - thinkingWindowPrefixReserve
	if width < minThinkingWindowWidth {
		width = minThinkingWindowWidth
	}
	return width
}

// resetThinkingState clears all thinking-block UI state so completed tasks or
// new sessions do not leave stale thinking preview lines in the status area.
func (m *Model) resetThinkingState() {
	m.thinkingTitle = ""
	m.thinkingReasoning = ""
	m.thinkingCompleted = false
	m.thinkingCollapsed = true
	m.thinkingWindow = nil
	m.thinkingPending = ""
}

// resetSpinnerToIdle stops the spinner in its static idle state.
func (m *Model) resetSpinnerToIdle() {
	m.spinnerStage = ""
	m.spinnerFrame = 0
	m.selectedVerb = ""
}

func (m *Model) advanceSpinner(stage string) {
	if stage != "" {
		m.spinnerStage = stage
	}
	m.spinnerFrame++
}

func (m *Model) currentSpinnerFrame() string {
	if m.currentPhase == phaseDone || m.currentPhase == phaseIdle {
		return ""
	}
	frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
	switch m.spinnerStage {
	case "thinking":
		return frame
	case "executing":
		return frame
	case "writing":
		return frame
	default:
		return frame
	}
}

// currentSpinnerVerb returns the current spinner verb for the active thinking cycle.
func (m *Model) currentSpinnerVerb() string {
	if m.currentPhase == phaseDone || m.currentPhase == phaseIdle {
		return ""
	}
	return m.selectedVerb
}

func (m *Model) renderContextBar() string {
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
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	var colorCode string
	switch {
	case pct < 0.6:
		colorCode = "82"
	case pct < 0.85:
		colorCode = "220"
	default:
		colorCode = "196"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colorCode)).Render(fmt.Sprintf("[%s] %d%%", bar, int(pct*100)))
}

func (m *Model) detectFileMentionContext() (bool, string) {
	text := m.input.Value()
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

func (m *Model) updateFilePickerState() {
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

func (m *Model) insertSelectedFile() {
	if len(m.filePickerResults) == 0 || m.filePickerCursor < 0 || m.filePickerCursor >= len(m.filePickerResults) {
		return
	}
	text := m.input.Value()
	idx := strings.LastIndex(text, "@")
	if idx < 0 {
		return
	}
	replacement := "@" + m.filePickerResults[m.filePickerCursor] + " "
	m.input.SetValue(text[:idx] + replacement)
	m.input.CursorEnd()
	m.adjustInputHeight()
}

func (m *Model) renderFilePicker() string {
	if !m.showingFilePicker {
		return ""
	}
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorDetail)).
		Padding(0, 1)
	lines := []string{fmt.Sprintf("# 文件: %s", m.filePickerQuery)}
	if len(m.filePickerResults) == 0 {
		lines = append(lines, "无匹配")
	} else {
		for i, path := range m.filePickerResults {
			prefix := "  "
			if i == m.filePickerCursor {
				prefix = "› "
			}
			lines = append(lines, prefix+path)
		}
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m *Model) View() tea.View { //nolint:revive
	var b strings.Builder

	if m.altScreenActive {
		v := m.renderFullscreenHistory()
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return m.configureView(v)
	}

	if m.showingCommands {
		m.renderCommandsPalette(&b)
		v := tea.NewView(b.String())
		v.MouseMode = tea.MouseModeCellMotion
		return m.configureView(v)
	}

	// If showing confirmation dialog, render it instead of normal input
	if m.showingConfirmation {
		m.renderConfirmationDialog(&b)
		v := tea.NewView(b.String())
		v.MouseMode = tea.MouseModeCellMotion
		return m.configureView(v)
	}

	// Render input section first so we can measure its physical height and
	// compute the message-window budget that fills the space above it.
	var inputBuf strings.Builder
	m.renderInputSection(&inputBuf)
	inputStr := inputBuf.String()

	// Banner: keep the static product banner above the input when there is
	// enough vertical room. On short terminals with messages, hide it so the
	// conversation still has a usable message window.
	bannerRendered := false
	if m.bannerArt != "" && m.termHeight > 0 {
		bannerLines := renderedLineCount(m.bannerArt)
		inputLines := renderedLineCount(inputStr)
		const minMessageLines = 3
		hasRoom := bannerLines+inputLines <= m.termHeight
		hasMessageRoom := m.messages.Len() == 0 || bannerLines+inputLines+minMessageLines <= m.termHeight
		if hasRoom && hasMessageRoom {
			b.WriteString(m.bannerArt)
			b.WriteString("\n")
			bannerRendered = true
		}
	}

	// Message window: fill the remaining terminal rows between the banner
	// (if rendered) and the input section with the most recent messages.
	if m.termHeight > 0 && m.messages.Len() > 0 {
		inputLines := renderedLineCount(inputStr)
		bannerLines := 0
		if bannerRendered {
			bannerLines = renderedLineCount(m.bannerArt) + 1 // +1 for the trailing "\n"
		}
		windowLines := m.termHeight - inputLines - bannerLines
		if windowLines > 0 {
			if window := m.renderMessageWindow(windowLines); window != "" {
				b.WriteString(window)
				b.WriteString("\n")
			}
		}
	}

	b.WriteString(inputStr)
	return m.configureView(tea.NewView(b.String()))
}

func renderedLineCount(s string) int {
	if s == "" {
		return 0
	}
	lines := strings.Count(s, "\n")
	if strings.HasSuffix(s, "\n") {
		return lines
	}
	return lines + 1
}

func (m *Model) configureView(v tea.View) tea.View {
	v.KeyboardEnhancements.ReportEventTypes = true
	v.ReportFocus = true
	v.WindowTitle = m.buildWindowTitle()
	return v
}

// renderInputSection renders the input and status section.
// It outputs a dynamic number of input rows for the textarea while keeping
// non-input lines capped at m.termWidth columns so that wide characters or
// long help text never trigger terminal auto-wrapping.
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
	if m.cronIndicator != "" {
		status = m.cronIndicator + " | " + status
	}
	statusLine := statusStyle.Render("[状态] " + status)
	spinner := m.currentSpinnerFrame()
	verb := m.currentSpinnerVerb()
	var spinnerPart string
	if spinner != "" {
		if verb != "" {
			spinnerPart = "  " + spinner + " " + verb
		} else {
			spinnerPart = "  " + spinner
		}
	}
	if contextBar := m.renderContextBar(); contextBar != "" {
		spinnerPart += "  " + contextBar
	}
	permTag := m.renderPermissionModeTag()
	var statusWithPerm string
	if permTag != "" {
		statusWithPerm = statusLine + spinnerPart + "  " + permTag
	} else {
		statusWithPerm = statusLine + spinnerPart
	}
	if m.termWidth > 0 && lipgloss.Width(statusWithPerm) > m.termWidth {
		// No tail: status/token lines are structured data; a hard cut is
		// cleaner than appending "…" which may misalign ANSI sequences.
		statusWithPerm = xansi.Truncate(statusWithPerm, m.termWidth, "")
	}
	b.WriteString(statusWithPerm + "\n")

	// Line 3: token status / connection status (always rendered)
	tokenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorStatus))
	tokenText := m.tokenStatus
	if tokenText == "" {
		tokenText = "---"
	}
	if m.connectionState != "" {
		tokenText += " | ● " + m.connectionState
		if m.connectionDetail != "" {
			tokenText += " " + m.connectionDetail
		}
	}
	if m.cwd != "" {
		base := m.cwd
		if idx := strings.LastIndex(base, "/"); idx >= 0 && idx < len(base)-1 {
			parent := base[:idx]
			if pidx := strings.LastIndex(parent, "/"); pidx >= 0 {
				base = base[pidx+1:]
			} else {
				base = base[idx+1:]
			}
		}
		tokenText += " | " + SafeChar("folder_prefix", m.termCap) + base
	}
	if m.apiBaseURL != "" {
		tokenText += " | " + SafeChar("globe_prefix", m.termCap) + m.apiBaseURL
	}
	tokenLine := tokenStyle.Render("[令牌] " + tokenText)
	if m.termWidth > 0 && lipgloss.Width(tokenLine) > m.termWidth {
		// No tail: structured data line; hard cut avoids appending "…".
		tokenLine = xansi.Truncate(tokenLine, m.termWidth, "")
	}
	b.WriteString(tokenLine + "\n")

	if !m.thinkingCollapsed && strings.TrimSpace(m.thinkingReasoning) != "" {
		m.renderExpandedThinking(b)
	} else if len(m.thinkingWindow) > 0 {
		thinkingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorSystem))
		for _, line := range m.thinkingWindow {
			rendered := thinkingStyle.Render("[思考] " + line)
			if m.termWidth > 0 && lipgloss.Width(rendered) > m.termWidth {
				rendered = xansi.Truncate(rendered, m.termWidth, "")
			}
			b.WriteString(rendered + "\n")
		}
	}

	if picker := m.renderFilePicker(); picker != "" {
		b.WriteString(picker + "\n")
	}

	// Line 4: input prompt
	inputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser))
	prefix := inputStyle.Render("💬 ")
	inputView := m.input.View()
	inputLines := strings.Split(inputView, "\n")
	for i, line := range inputLines {
		var inputLine string
		if i == 0 {
			inputLine = prefix + line
		} else {
			inputLine = "   " + line
		}
		if m.termWidth > 0 && lipgloss.Width(inputLine) > m.termWidth {
			// Hard-cut input rows without a suffix so the physical row never
			// exceeds termWidth in inline renderer mode.
			inputLine = xansi.Truncate(inputLine, m.termWidth, "")
		}
		b.WriteString(inputLine + "\n")
	}

	// Line 5: help text (width-adaptive to prevent terminal auto-wrapping)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	b.WriteString(helpStyle.Render(m.buildHelpText()) + "\n")
}

func (m *Model) renderExpandedThinking(b *strings.Builder) {
	thinkingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorSystem))
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
	for _, line := range lines[start:] {
		rendered := thinkingStyle.Render("[思考] " + strings.TrimSpace(line))
		if m.termWidth > 0 && lipgloss.Width(rendered) > m.termWidth {
			rendered = xansi.Truncate(rendered, m.termWidth, "")
		}
		b.WriteString(rendered + "\n")
	}
}

func (m *Model) renderFullscreenHistory() tea.View {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorInfoTitle)).
		Render("nano 历史浏览  (↑↓/jk 选择 · g/G 跳转 · Ctrl+Y 复制选中行 · Esc/q/Ctrl+F 退出)")
	if m.termWidth > 0 && lipgloss.Width(title) > m.termWidth {
		title = xansi.Truncate(title, m.termWidth, "")
	}
	b.WriteString(title + "\n\n")

	viewportHeight := m.historyViewportHeight()
	nMsg := m.historyMsgLen()
	if nMsg == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("暂无历史记录") + "\n")
		return tea.NewView(b.String())
	}
	maxOffset := maxInt(0, nMsg-viewportHeight)
	m.historyScrollOffset = clampInt(m.historyScrollOffset, 0, maxOffset)
	end := m.historyScrollOffset + viewportHeight
	if end > nMsg {
		end = nMsg
	}
	for i := m.historyScrollOffset; i < end; i++ {
		msg := m.messages.Get(i)
		if msg == nil {
			continue
		}
		// Use the shared renderedMessageLines cache so formatting work done
		// by the inline window is reused here. Take only the first line so
		// the history browser shows exactly one row per message regardless
		// of how many wrapped lines the message occupies in the inline view.
		renderedLines := m.renderedMessageLines(msg)
		line := ""
		if len(renderedLines) > 0 {
			line = renderedLines[0]
		}
		if m.termWidth > 0 && xansi.StringWidth(line) > m.termWidth {
			line = xansi.Truncate(line, m.termWidth, "")
		}
		if i == m.historySelected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorButtonFg)).
				Background(lipgloss.Color(colorDimBg)).
				Render(line)
		}
		b.WriteString(line + "\n")
	}
	return tea.NewView(b.String())
}

// historyMsgLen returns the number of displayable messages in the store.
func (m *Model) historyMsgLen() int {
	return m.messages.Len()
}

func (m *Model) historyViewportHeight() int {
	if m.termHeight <= 3 {
		return 10
	}
	return m.termHeight - 3
}

func (m *Model) enterFullscreenHistory() {
	m.altScreenActive = true
	viewportHeight := m.historyViewportHeight()
	nMsg := m.historyMsgLen()
	m.historyScrollOffset = maxInt(0, nMsg-viewportHeight)
	m.historySelected = clampInt(nMsg-1, 0, maxInt(0, nMsg-1))
}

func (m *Model) handleFullscreenHistoryKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "ctrl+f":
		m.altScreenActive = false
	case "up", "k":
		m.moveHistorySelection(-1)
	case "down", "j":
		m.moveHistorySelection(1)
	case "g":
		m.historySelected = 0
		m.ensureHistorySelectionVisible()
	case "G":
		m.historySelected = maxInt(0, m.historyMsgLen()-1)
		m.ensureHistorySelectionVisible()
	case "ctrl+y":
		if content := m.getSelectedHistoryContent(); content != "" {
			return tea.SetClipboard(content)
		}
	}
	return nil
}

func (m *Model) scrollFullscreenHistory(delta int) {
	nMsg := m.historyMsgLen()
	maxOffset := maxInt(0, nMsg-m.historyViewportHeight())
	m.historyScrollOffset = clampInt(m.historyScrollOffset+delta, 0, maxOffset)
	m.historySelected = clampInt(m.historySelected+delta, 0, maxInt(0, nMsg-1))
	m.ensureHistorySelectionVisible()
}

func (m *Model) moveHistorySelection(delta int) {
	nMsg := m.historyMsgLen()
	if nMsg == 0 {
		return
	}
	m.historySelected = clampInt(m.historySelected+delta, 0, nMsg-1)
	m.ensureHistorySelectionVisible()
}

func (m *Model) ensureHistorySelectionVisible() {
	nMsg := m.historyMsgLen()
	if nMsg == 0 {
		m.historyScrollOffset = 0
		m.historySelected = 0
		return
	}
	viewportHeight := m.historyViewportHeight()
	if m.historySelected < m.historyScrollOffset {
		m.historyScrollOffset = m.historySelected
	}
	if m.historySelected >= m.historyScrollOffset+viewportHeight {
		m.historyScrollOffset = m.historySelected - viewportHeight + 1
	}
	maxOffset := maxInt(0, nMsg-viewportHeight)
	m.historyScrollOffset = clampInt(m.historyScrollOffset, 0, maxOffset)
}

func (m *Model) getSelectedHistoryContent() string {
	nMsg := m.historyMsgLen()
	if nMsg == 0 {
		return ""
	}
	idx := clampInt(m.historySelected, 0, nMsg-1)
	msg := m.messages.Get(idx)
	if msg == nil {
		return ""
	}
	return msg.Content
}

// lastAssistantReply returns the most recent assistant message content,
// walking the MessageStore backwards and skipping non-assistant roles.
func (m *Model) lastAssistantReply() string {
	for i := m.messages.Len() - 1; i >= 0; i-- {
		msg := m.messages.Get(i)
		if msg == nil {
			continue
		}
		if msg.Role == "assistant" || msg.Role == "assistant_stream" {
			return msg.Content
		}
	}
	return ""
}

func (m *Model) buildWindowTitle() string {
	switch m.currentPhase {
	case phaseIdle, phaseDone:
		return "nano · 就绪"
	case phaseProcessing:
		return "nano · 处理中..."
	case phaseThinking:
		return "nano · 思考中..."
	case phaseToolCall:
		return "nano · 工具执行中"
	case phaseAwaitingApproval:
		return "nano · ⚠️ 等待确认"
	case phaseResponse:
		return "nano · 回复中..."
	default:
		return "nano"
	}
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		// Empty ranges can occur when callers clamp against an empty viewport
		// or list; use the lower bound as a safe no-op position.
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// buildHelpText returns help text that fits within the terminal width.
// Progressively shorter versions are tried before falling back to a hard
// truncation. This prevents terminal auto-wrapping which would cause the
// renderer's line count to differ from actual displayed lines, leading to
// duplicate View output in non-AltScreen (inline) mode.
// When showHelp is false only a compact "? 帮助" hint is returned so the user
// knows the key exists but the line stays minimal.
func (m *Model) buildHelpText() string {
	if !m.showHelp {
		hint := "? 帮助"
		if m.termWidth > 0 && lipgloss.Width(hint) <= m.termWidth {
			return hint
		}
		return ""
	}
	full := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 搜索历史 | Ctrl+F 搜索 | Ctrl+L 新会话 | Ctrl+P 命令 | Ctrl+C 退出 | Tab 补全 | [ 滚动 | PgUp/PgDn 翻页 | ↑↓ 历史 | ? 收起"
	if m.termWidth <= 0 || lipgloss.Width(full) <= m.termWidth {
		return full
	}
	short := "Enter 发送 | Ctrl+J 换行 | Ctrl+R 搜索 | Ctrl+F 搜索 | Ctrl+L 新会话 | Ctrl+P 命令 | PgUp/PgDn 翻页 | Tab 补全 | ? 收起"
	if lipgloss.Width(short) <= m.termWidth {
		return short
	}
	minimal := "Enter发送 | ^J换行 | ^R搜索 | ^F搜索 | ^L新会话 | PgUp/PgDn | ?收起"
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

// safeWrapWidth returns the effective word-wrap width accounting for formatted
// emoji prefixes so wrapped output never exceeds termWidth on the first line.
func (m *Model) safeWrapWidth() int {
	if m.termWidth <= 0 {
		return defaultMarkdownRenderWidth
	}
	return maxInt(m.termWidth-formattedLinePrefixReserve, 1)
}

// safeWrapWidthForRole returns the word-wrap budget appropriate for the given
// message role. Roles whose formatLine output includes an emoji prefix reserve
// formattedLinePrefixReserve columns so the first rendered line never exceeds
// termWidth. Roles without a prefix (assistant_stream, and the catch-all
// default case) use the full terminal width.
func (m *Model) safeWrapWidthForRole(role string) int {
	switch role {
	case "assistant_stream", "":
		if m.termWidth <= 0 {
			return defaultMarkdownRenderWidth
		}
		return m.termWidth
	default:
		return m.safeWrapWidth()
	}
}

// wrapFormattedLineForRole wraps and truncates line using the wrap budget
// appropriate for the given role. Roles without an emoji prefix (e.g.
// assistant_stream) use the full terminal width via safeWrapWidthForRole;
// emoji-prefix roles reserve formattedLinePrefixReserve columns.
func (m *Model) wrapFormattedLineForRole(role, line string) string {
	return truncateLines(wordwrap.String(line, m.safeWrapWidthForRole(role)), m.termWidth)
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

type SpinnerTickMsg time.Time

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(spinnerTickInterval, func(t time.Time) tea.Msg {
		return SpinnerTickMsg(t)
	})
}

func (m *Model) shouldAnimateSpinner() bool {
	return m.isSpinnerPhase()
}

func (m *Model) isSpinnerPhase() bool {
	return m.isActivePhase() || m.currentPhase == phaseProcessing || m.currentPhase == phaseResponse
}

func (m *Model) shouldRunSpinner() bool {
	return m.terminalFocused && m.isSpinnerPhase()
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
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	Peak              float64
	ContextWindowMax  int
	ContextUsedTokens int
}

// ShowConfirmationMsg is sent via p.Send() to trigger the confirmation dialog
// from outside the Bubble Tea event loop (e.g., from the approval handler goroutine).
type ShowConfirmationMsg struct {
	Message  string
	ToolInfo map[string]interface{}
	// Callback is called with the user's yes/no decision for options 0 and 1.
	Callback func(bool)
	// AlwaysCallback, if set, is called instead of Callback(true) when the user
	// chooses option 2 ("始终允许"). This allows callers (e.g. BubbleTeaAdapter)
	// to send Always:true in the outbound approval without also sending a plain
	// Allow:true approval. The existing allowlistHandler on the model is still
	// invoked after AlwaysCallback to add the rule to the local session list.
	AlwaysCallback func()
}

type ConnectionStatusMsg struct {
	State  string
	Detail string
}

type NoticeMsg string

type MailboxMsg struct {
	From, To, Kind, Preview string
}

type IdleNotifyMsg struct {
	Agent, Summary string
}

type SpawnTeammateMsg struct {
	Agent, Topic, SessionID string
}

// TaskCompletionMsg signals that the entire agent turn has completed.
type TaskCompletionMsg struct {
	Reason string
}

// ImagePasteSuccessMsg is sent when an image is successfully pasted from clipboard
type ImagePasteSuccessMsg struct {
	Image llm.MultimodalImage
	Path  string
}

// ImagePasteErrorMsg is sent when image paste fails
type ImagePasteErrorMsg struct {
	Error error
}

func formatLine(kind, s string) string {
	switch kind {
	case "assistant":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorAssistant)).Render("🤖 " + strings.TrimRight(s, "\n"))
	case "assistant_stream":
		return renderAssistantStreamText(s)
	case "thinking":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorSystem)).Bold(true).Render("🧠 " + strings.TrimRight(s, "\n"))
	case "user":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorUser)).Bold(true).Render("👤 " + strings.TrimRight(s, "\n"))
	case "tool":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorTool)).Render("🛠 " + strings.TrimRight(s, "\n"))
	case "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorError)).Bold(true).Render("❌ " + strings.TrimRight(s, "\n"))
	case "system":
		return lipgloss.NewStyle().Foreground(lipgloss.Color(colorSystem)).Bold(true).Render("⚙ " + strings.TrimRight(s, "\n"))
	default:
		return strings.TrimRight(s, "\n")
	}
}

func renderAssistantStreamText(s string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorAssistant))
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
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

// SetBannerArt stores the rendered product banner artwork that the inline
// View() displays above the input area while the conversation is empty.
// Callers (the UI adapter / CLI bootstrap) pass the static last frame of the
// startup banner animation so the TUI is never blank of product identity.
// Pass an empty string to disable banner display.
func (m *Model) SetBannerArt(art string) {
	m.bannerArt = art
}

// SetPermissionManager wires a permission.Manager so that slash commands
// (/yolo, /permission, /allow, /disallow, /permissions) work in Bubble Tea mode.
func (m *Model) SetPermissionManager(mgr *permission.Manager) {
	m.permissionManager = mgr
	if mgr != nil {
		m.permissionMode = string(mgr.GetMode())
	}
}

// SetPersistentAllowlist wires the persistent allowlist store and workdir
// so /disallow can remove rules from persistent storage.
func (m *Model) SetPersistentAllowlist(store *permission.PersistentAllowlistStore, workdir string) {
	m.persistentAllowlist = store
	m.workdir = workdir
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

// SetNewSessionHandler wires the callback used by Ctrl+L / /clear to create
// a new agent session.
func (m *Model) SetNewSessionHandler(h func() string) {
	m.newSessionHandler = h
}

// SetTeamName scopes locally-handled slash commands such as /teammates and
// /agents to a specific team. Pass an empty string to list all teams.
func (m *Model) SetTeamName(name string) {
	m.teamName = name
	// Invalidate the dispatcher so it picks up the new team name on next use.
	m.localDispatcher = nil
}

func (m *Model) SetModelLister(fn func() string) {
	m.modelLister = fn
	m.localDispatcher = nil
}

func (m *Model) SetSkillLister(fn func() string) {
	m.skillLister = fn
	m.localDispatcher = nil
}

func (m *Model) SetModelStatusGetter(fn func() string) {
	m.modelStatusGetter = fn
	m.localDispatcher = nil
}

func (m *Model) SetModelSwitcher(fn func(string) string) {
	m.modelSwitcher = fn
	m.localDispatcher = nil
}

func (m *Model) SetModelFallbackHandler(fn func(string) string) {
	m.modelFallbackHandler = fn
	m.localDispatcher = nil
}

func (m *Model) SetModelDoctor(fn func(string) string) {
	m.modelDoctor = fn
	m.localDispatcher = nil
}

func (m *Model) SetContextStatusGetter(fn func() string) {
	m.contextStatusGetter = fn
	m.localDispatcher = nil
}

func (m *Model) SetDoctorReporter(fn func() string) {
	m.doctorReporter = fn
	m.localDispatcher = nil
}

func (m *Model) SetEventsQuerier(fn func(string) string) {
	m.eventsQuerier = fn
	m.localDispatcher = nil
}

func (m *Model) SetAuditQuerier(fn func(string) string) {
	m.auditQuerier = fn
	m.localDispatcher = nil
}

// SetRoutinesLister wires a callback for /routines list.
func (m *Model) SetRoutinesLister(fn func() string) {
	m.routinesLister = fn
	m.localDispatcher = nil
}

// SetRunningStatusLister wires a callback for /routines status.
func (m *Model) SetRunningStatusLister(fn func() string) {
	m.runningStatusLister = fn
	m.localDispatcher = nil
}

func (m *Model) SetRoutinesAdder(fn func(string) string) {
	m.routinesAdder = fn
	m.localDispatcher = nil
}

func (m *Model) SetRoutinesRemover(fn func(string) string) {
	m.routinesRemover = fn
	m.localDispatcher = nil
}

func (m *Model) SetRoutinesPauser(fn func(string) string) {
	m.routinesPauser = fn
	m.localDispatcher = nil
}

func (m *Model) SetRoutinesResumer(fn func(string) string) {
	m.routinesResumer = fn
	m.localDispatcher = nil
}

func (m *Model) SetRoutinesRunner(fn func(string) string) {
	m.routinesRunner = fn
	m.localDispatcher = nil
}

// getLocalDispatcher returns the lazily-constructed LocalDispatcher.
func (m *Model) getLocalDispatcher() *slash.LocalDispatcher {
	if m.localDispatcher == nil {
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
		m.localDispatcher = d
	}
	return m.localDispatcher
}

// handleLocalSlashCommand dispatches the input through the shared
// LocalDispatcher. It returns (true, cmd) when the dispatcher handled the
// input (in which case the resulting message has already been appended to
// the conversation), or (false, nil) for the caller to fall through to its
// existing pipeline.
//
// Note: this helper does not surface the dispatcher's ShouldSubmit branch.
// Callers that need to forward a rewritten command to the agent should call
// the dispatcher directly and use recordLocalSlashResult to render local
// messages.
func (m *Model) handleLocalSlashCommand(input string) (bool, tea.Cmd) {
	r := m.getLocalDispatcher().Dispatch(input)
	if !r.Handled {
		return false, nil
	}
	return m.recordLocalSlashResult(r)
}

// recordLocalSlashResult appends a dispatcher Result message to the
// transcript, mapping the dispatcher level to a UI role.
func (m *Model) recordLocalSlashResult(r slash.Result) (bool, tea.Cmd) {
	level := r.Level
	if level == "" {
		level = "info"
	}
	uiLevel := level
	if level == "warning" {
		uiLevel = "info" // renderPermissionFeedback only knows success/error/info
	}
	role := "system"
	if uiLevel == "error" {
		role = "error"
	}
	m.recordMessage(role, r.Message)
	return true, nil
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
			m.recordPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			return true, nil
		}
		pm.SetMode(permission.ModeYOLO)
		m.permissionMode = string(permission.ModeYOLO)
		m.recordPermissionFeedback("success",
			"⚡ YOLO 模式已激活",
			"所有工具将自动执行，无需确认。使用 /permission default 恢复。")
		return true, nil

	case lower == "/plan":
		if pm == nil {
			m.recordPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			return true, nil
		}
		pm.SetMode(permission.ModePlan)
		m.permissionMode = string(permission.ModePlan)
		m.recordPermissionFeedback("success",
			"📋 Plan 模式已激活",
			"只允许只读工具执行（用于安全代码分析）。使用 /permission default 恢复。")
		return true, nil

	case lower == "/permissions":
		m.recordMessage("system", m.renderPermissionsPanel())
		return true, nil

	case strings.HasPrefix(lower, "/permission "):
		arg := strings.TrimSpace(input[len("/permission "):])
		if pm == nil {
			m.recordPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			return true, nil
		}
		// Normalize arg to lowercase for case-insensitive matching
		mode := permission.PermissionMode(strings.ToLower(arg))
		// Map lowercase to canonical mode strings
		switch mode {
		case permission.PermissionMode(strings.ToLower(string(permission.ModeDefault))):
			mode = permission.ModeDefault
		case permission.PermissionMode(strings.ToLower(string(permission.ModeAcceptEdits))):
			mode = permission.ModeAcceptEdits
		case permission.PermissionMode(strings.ToLower(string(permission.ModePlan))):
			mode = permission.ModePlan
		case permission.PermissionMode(strings.ToLower(string(permission.ModeAuto))):
			mode = permission.ModeAuto
		case permission.PermissionMode(strings.ToLower(string(permission.ModeYOLO))):
			mode = permission.ModeYOLO
		}
		switch mode {
		case permission.ModeDefault, permission.ModeAcceptEdits, permission.ModePlan, permission.ModeAuto, permission.ModeYOLO:
			pm.SetMode(mode)
			m.permissionMode = string(mode)
			m.recordPermissionFeedback("success",
				fmt.Sprintf("✅ 权限模式已切换为：%s", mode), "")
			return true, nil
		default:
			m.recordPermissionFeedback("error",
				fmt.Sprintf("❌ 未知模式：%s", arg),
				"可选：default / acceptEdits / plan / auto / yolo")
			return true, nil
		}

	case strings.HasPrefix(lower, "/allow "):
		raw := strings.TrimSpace(input[len("/allow "):])
		if raw == "" {
			m.recordPermissionFeedback("error",
				"❌ 规则不能为空",
				"示例：/allow Bash(git *) 或 /allow write_file(*.go)")
			return true, nil
		}
		if pm == nil {
			m.recordPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			return true, nil
		}
		rule := permission.ParseRule(raw)
		if rule.ToolName == "" {
			m.recordPermissionFeedback("error",
				fmt.Sprintf("❌ 无效规则：%q", raw), "")
			return true, nil
		}
		pm.GetSessionAllowlist().AddRule(rule)
		m.recordPermissionFeedback("success",
			fmt.Sprintf("✅ 已添加白名单规则：%s", rule.RawPattern), "")
		return true, nil

	case strings.HasPrefix(lower, "/disallow "):
		raw := strings.TrimSpace(input[len("/disallow "):])
		if pm == nil {
			m.recordPermissionFeedback("error", "❌ 权限管理器未初始化", "")
			return true, nil
		}
		pm.GetSessionAllowlist().RemoveRule(raw)
		// Also remove from persistent storage
		if m.persistentAllowlist != nil && m.workdir != "" {
			if _, err := m.persistentAllowlist.RemoveRuleForWorkdir(m.workdir, raw); err != nil {
				logger.Warnf("Failed to remove persistent allowlist rule %q: %v", raw, err)
			}
		}
		m.recordPermissionFeedback("success",
			fmt.Sprintf("🗑️ 已移除白名单规则：%s", raw), "")
		return true, nil

	case strings.HasPrefix(lower, "/think"):
		// Handle /think command via Engine
		if m.engine == nil {
			m.recordPermissionFeedback("error", "❌ Engine 未初始化", "")
			return true, nil
		}
		// Extract args after /think
		args := strings.TrimSpace(input[len("/think"):])
		m.recordPermissionFeedback("info", m.engine.HandleThinkCommand(args), "")
		return true, nil

	case lower == "/clear", lower == "/new":
		// /clear or /new: Start a new session (clear context) - equivalent to Ctrl+L
		return true, m.clearSessionCmd()
	}

	return false, nil
}

// recordPermissionFeedback records the permission-feedback into the shared
// MessageStore. The returned string is the already-styled feedback line for
// callers that need the raw text.
//
// The role used is derived from `level`: "error" becomes the "error" role,
// everything else becomes "system".
func (m *Model) recordPermissionFeedback(level, title, detail string) string {
	line := m.renderPermissionFeedback(level, title, detail)
	content := title
	if detail != "" {
		content = title + "\n" + detail
	}
	role := "system"
	if level == "error" {
		role = "error"
	}
	m.recordMessage(role, content)
	return line
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
	// NOTE: lipgloss BorderStyle causes ghost artifacts in BubbleTea v2 inline renderer,
	// even in fullscreen history view (altScreenActive mode).
	return lipgloss.NewStyle().
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
	if permission.PermissionMode(mode) == permission.ModeDefault {
		return ""
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

// cyclePermissionMode returns the next mode in the Shift+Tab cycle.
// Cycle: default → acceptEdits → plan → auto → yolo → default.
func cyclePermissionMode(current string) permission.PermissionMode {
	order := []permission.PermissionMode{
		permission.ModeDefault,
		permission.ModeAcceptEdits,
		permission.ModePlan,
		permission.ModeAuto,
		permission.ModeYOLO,
	}
	cur := permission.PermissionMode(current)
	for i, mode := range order {
		if mode == cur {
			return order[(i+1)%len(order)]
		}
	}
	return permission.ModeDefault
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
		modes := []string{"default", "acceptEdits", "plan", "auto", "yolo"}
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
	m.confirmationAlwaysCallback = nil
	m.confirmationSelected = 0
	m.confirmationIsPaste = false
	m.pendingPaste = ""
	m.confirmationButtons = nil
	// After approval/rejection, agent typically resumes work.
	m.currentPhase = phaseProcessing
	m.status = m.formatStatusForPhase(phaseProcessing, "")
}

// renderConfirmationDialog renders the confirmation dialog
func (m *Model) renderConfirmationDialog(b *strings.Builder) {
	m.confirmationButtons = nil
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

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorSecondary)).
		Background(lipgloss.Color(colorDimBg)).
		Padding(0, 2)

	var yesButton, noButton string
	switch m.confirmationSelected {
	case 0:
		yesButton = yesStyle.Render("✓ 确认")
		noButton = normalStyle.Render("✗ 取消")
	default:
		yesButton = normalStyle.Render("✓ 确认")
		noButton = noStyle.Render("✗ 取消")
	}

	buttonsLine := lipgloss.JoinHorizontal(lipgloss.Left, yesButton, "  ", noButton)
	buttonY := strings.Count(b.String(), "\n")
	yesWidth := lipgloss.Width(yesButton)
	noWidth := lipgloss.Width(noButton)
	m.confirmationButtons = []hitBox{
		{x0: 0, x1: yesWidth, y: buttonY},
		{x0: yesWidth + 2, x1: yesWidth + 2 + noWidth, y: buttonY},
	}
	if !m.confirmationIsPaste {
		alwaysStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorButtonFg)).
			Background(lipgloss.Color(colorAlwaysButtonBg)).
			Padding(0, 2).
			Bold(true)
		alwaysButton := normalStyle.Render("★ 始终允许")
		if m.confirmationSelected == 2 {
			alwaysButton = alwaysStyle.Render("★ 始终允许")
		}
		alwaysWidth := lipgloss.Width(alwaysButton)
		buttonsLine = lipgloss.JoinHorizontal(lipgloss.Left, yesButton, "  ", noButton, "  ", alwaysButton)
		m.confirmationButtons = append(m.confirmationButtons, hitBox{
			x0: yesWidth + 2 + noWidth + 2,
			x1: yesWidth + 2 + noWidth + 2 + alwaysWidth,
			y:  buttonY,
		})
	}
	b.WriteString(buttonsLine + "\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	b.WriteString(helpStyle.Render("← → 或 h l 选择，Enter 确认，Esc 取消") + "\n")
}

func (m *Model) hitTestConfirmationButton(x, y int) int {
	for i, box := range m.confirmationButtons {
		if y == box.y && x >= box.x0 && x < box.x1 {
			return i
		}
	}
	return -1
}

func (m *Model) hitTestCommandItem(x, y int) int {
	for _, box := range m.commandItems {
		if y == box.y && x >= box.x0 && (box.x1 == 0 || x < box.x1) {
			return box.index
		}
	}
	return -1
}

func (m *Model) openCommandsPalette() {
	m.loadCommands()
	m.showingCommands = true
	if m.commandsIndex >= len(m.commands) {
		m.commandsIndex = 0
	}
	m.commandsScrollOffset = 0 // reset scroll on every open
}

func (m *Model) moveCommandSelection(delta int) {
	if len(m.commands) == 0 {
		return
	}
	m.commandsIndex = clampInt(m.commandsIndex+delta, 0, len(m.commands)-1)
	maxOffset := maxInt(0, len(m.commands)-commandsPaletteVisibleRows)
	if m.commandsIndex-m.commandsScrollOffset < commandsPaletteScrollPadding {
		m.commandsScrollOffset = maxInt(0, m.commandsIndex-commandsPaletteScrollPadding)
	}
	if m.commandsIndex-m.commandsScrollOffset >= commandsPaletteVisibleRows-commandsPaletteScrollPadding {
		m.commandsScrollOffset = m.commandsIndex - commandsPaletteVisibleRows + commandsPaletteScrollPadding + 1
		if m.commandsScrollOffset > maxOffset {
			m.commandsScrollOffset = maxOffset
		}
	}
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
	m.commandItems = nil

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
		slash.CategoryRoutines:   "调度",
		slash.CategoryOpenSpec:   "OpenSpec",
		slash.CategoryCustom:     "自定义",
	}
	categoryColors := map[slash.Category]string{
		slash.CategoryPermission: colorWarning,
		slash.CategorySkill:      colorSuccess,
		slash.CategoryRoutines:   colorStatus,
		slash.CategoryOpenSpec:   colorOpenSpec,
		slash.CategoryCustom:     colorSystem,
	}

	// Render commands with dynamic row calculation that accounts for category headers.
	// We iterate through all commands starting from the scroll offset, rendering
	// each command and its category header (if it's the first in that category).
	// We stop when we've rendered visibleRows worth of content (commands + headers).
	currentCat := slash.Category("")
	renderedRows := 0
	startIdx := m.commandsScrollOffset
	row := 2

	for i := startIdx; i < total && renderedRows < visibleRows; i++ {
		it := m.commands[i]

		// If entering a new category, render the header (counts as 2 rows: blank line + header)
		if it.Category != currentCat {
			currentCat = it.Category
			label := categoryLabels[currentCat]
			color := categoryColors[currentCat]
			catStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))
			b.WriteString("\n" + catStyle.Render("── "+label+" ──") + "\n")
			renderedRows += 2
			row += 2

			// If we've already hit the limit with just the header, stop
			if renderedRows >= visibleRows {
				break
			}
		}

		// Render the command item (counts as 1 row)
		prefix := "  "
		if i == m.commandsIndex {
			prefix = "> "
		}
		line := fmt.Sprintf("%s/%s  %s\n", prefix, it.Name, it.Description)
		if it.Category == slash.CategoryCustom && it.Source != "" {
			line = fmt.Sprintf("%s/%s  [%s] %s\n", prefix, it.Name, it.Source, it.Description)
		}
		b.WriteString(line)
		m.commandItems = append(m.commandItems, commandHitBox{
			hitBox: hitBox{x0: 0, x1: xansi.StringWidth(strings.TrimRight(line, "\n")), y: row},
			index:  i,
		})
		renderedRows++
		row++
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
		box := lipgloss.NewStyle().Padding(1, 2).Render(pb.String())
		b.WriteString(box + "\n" + help + "\n")
	}
}

// handleImagePaste handles image paste events from clipboard
func (m *Model) handleImagePaste() tea.Cmd {
	return func() tea.Msg {
		imgData, err := clipboard.ReadImage()
		if err != nil {
			return ImagePasteErrorMsg{Error: err}
		}

		path, err := m.attachmentMgr.SaveImage(imgData, "image/png")
		if err != nil {
			return ImagePasteErrorMsg{Error: err}
		}

		img, err := m.attachmentMgr.ToMultimodalImage(path)
		if err != nil {
			return ImagePasteErrorMsg{Error: err}
		}

		return ImagePasteSuccessMsg{Image: img, Path: path}
	}
}

// handleFilePaste handles file paste events from clipboard
func (m *Model) handleFilePaste() tea.Cmd {
	return func() tea.Msg {
		filePaths, err := clipboard.ReadFilePaths()
		if err != nil || len(filePaths) == 0 {
			return ImagePasteErrorMsg{Error: fmt.Errorf("no files in clipboard")}
		}

		// Process first file only for now
		srcPath := filePaths[0]
		if !attachment.IsImageFile(srcPath) {
			return ImagePasteErrorMsg{Error: fmt.Errorf("file is not an image: %s", srcPath)}
		}

		destPath, err := m.attachmentMgr.SaveFile(srcPath)
		if err != nil {
			return ImagePasteErrorMsg{Error: err}
		}

		img, err := m.attachmentMgr.ToMultimodalImage(destPath)
		if err != nil {
			return ImagePasteErrorMsg{Error: err}
		}

		return ImagePasteSuccessMsg{Image: img, Path: destPath}
	}
}

// updateImageIndicator updates the UI indicator showing attached images
func (m *Model) updateImageIndicator() {
	count := len(m.pendingImages)
	if count == 0 {
		m.imageIndicator = ""
		return
	}
	m.imageIndicator = fmt.Sprintf("📎 %d image(s) attached", count)
}

// SetAttachmentManager sets the attachment manager for this model
func (m *Model) SetAttachmentManager(mgr *attachment.Manager) {
	m.attachmentMgr = mgr
}
