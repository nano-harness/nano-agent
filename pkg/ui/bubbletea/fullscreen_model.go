package bubbletea

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/nano-harness/nano-agent/pkg/ui/eventsource"
)

// FullscreenModel is the fullscreen TUI model with virtual scrolling.
type FullscreenModel struct {
	// Message data
	messages []*FormattedMessage

	// Virtual scrolling
	viewport *ViewportState

	// Input area
	textarea textarea.Model

	// Streaming state
	streamingBuf strings.Builder
	isStreaming  bool

	// Terminal dimensions
	termWidth  int
	termHeight int

	// Shared state from inline model
	outbound func(eventsource.Outbound) error
	cwd      string
	apiURL   string

	// Display state
	ready bool
}

// NewFullscreenModel creates a new fullscreen model.
func NewFullscreenModel(cwd, apiURL string) *FullscreenModel {
	ta := textarea.New()
	ta.Placeholder = "输入消息...  (Enter 发送, Shift+Enter 换行, Ctrl+C 退出)"
	ta.SetHeight(minInputHeight)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	applyTextareaTheme(&ta)
	ta.Focus()

	return &FullscreenModel{
		messages:   make([]*FormattedMessage, 0),
		viewport:   NewViewportState(10), // Initial height, will be updated
		textarea:   ta,
		cwd:        cwd,
		apiURL:     apiURL,
		termWidth:  80,
		termHeight: 24,
		ready:      false,
	}
}

// Init initializes the model.
func (m *FullscreenModel) Init() tea.Cmd {
	// Ensure the textarea is focused so it receives every keystroke, including
	// characters like j/k/g/G that previously collided with vim-style scroll
	// shortcuts. Without this the input field silently swallowed those keys.
	m.textarea.Focus()
	return textarea.Blink
}

// Update handles messages and updates the model.
func (m *FullscreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.ready = true

		// The textarea sits inside the floating input panel which has a
		// rounded border (2 cols), 2 cols of horizontal margin and 1 col of
		// inner padding on each side via the textarea theme.
		taWidth := m.termWidth - 4 - 2 - 2
		if taWidth < 10 {
			taWidth = 10
		}
		m.textarea.SetWidth(taWidth)

		// Update viewport height. The layout reserves rows for:
		//   1 status bar
		//   2 input panel border (top + bottom)
		//   N textarea rows
		//   1 help hint
		statusBarHeight := 1
		inputHeight := m.textarea.Height() + 2 // +2 for rounded border top/bottom
		helpHeight := 1
		m.viewport.SetViewportHeight(m.termHeight - statusBarHeight - inputHeight - helpHeight)

		// Re-render cached messages because their width depends on termWidth.
		for _, fm := range m.messages {
			m.renderMessage(fm)
		}
		m.updateViewportHeight()

		return m, nil

	case tea.KeyPressMsg:
		// Submission and exit shortcuts. All other keys (including j/k/g/G,
		// arrow keys, and printable characters) fall through to the
		// textarea so the input field can never swallow user typing.
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "enter":
			// Shift+Enter / Ctrl+J insert a newline; plain Enter submits.
			if msg.Code == tea.KeyEnter && msg.Mod&tea.ModShift != 0 {
				m.textarea.InsertString("\n")
				return m, nil
			}
			m.submitInput()
			return m, nil

		case "ctrl+j", "shift+enter":
			m.textarea.InsertString("\n")
			return m, nil

		case "ctrl+d":
			// Backup submit shortcut, preserved for muscle memory.
			m.submitInput()
			return m, nil

		// Scroll controls — none of these conflict with regular text input.
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

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.viewport.ScrollUp(historyWheelScrollLines)
		case tea.MouseWheelDown:
			m.viewport.ScrollDown(historyWheelScrollLines)
		}
		return m, nil

	case Message:
		// Handle incoming messages from adapter
		m.handleIncomingMessage(msg)
		return m, nil

	case ThinkingMsg:
		m.handleThinkingMessage(msg)
		return m, nil

	case ToolUseMsg:
		m.handleToolUseMessage(msg)
		return m, nil

	case StatusUpdate:
		// Handle status updates
		return m, nil
	}

	// Update textarea — every key event that wasn't explicitly handled above
	// (including printable characters and vim-style letters) reaches here.
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// submitInput sends the current textarea contents as a user message.
func (m *FullscreenModel) submitInput() {
	input := strings.TrimSpace(m.textarea.Value())
	if input == "" {
		return
	}
	m.addUserMessage(input)
	m.textarea.Reset()
	if m.outbound != nil {
		_ = m.outbound(eventsource.Outbound{
			Kind: "user_message",
			Text: input,
		})
	}
}

// View renders the fullscreen TUI.
func (m *FullscreenModel) View() tea.View {
	if !m.ready {
		return tea.NewView("Initializing...")
	}

	var b strings.Builder

	// 1. Status bar (1 line).
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

// renderStatusBar renders the top status bar with a colored background.
func (m *FullscreenModel) renderStatusBar() string {
	scrollPct := m.viewport.ScrollPercentage()
	scrollIndicator := fmt.Sprintf("%.0f%%", scrollPct)
	if m.viewport.IsSticky {
		scrollIndicator += " ↓"
	}

	indicator := lipgloss.NewStyle().
		Foreground(lipgloss.Color(fsColorAccent)).
		Bold(true).
		Render("●")

	status := fmt.Sprintf("%s nano-agent   messages: %d   scroll: %s",
		indicator, len(m.messages), scrollIndicator)
	return fsStatusBarStyle(m.termWidth).Render(status)
}

// renderMessageArea renders the virtual scrolling message area.
func (m *FullscreenModel) renderMessageArea() string {
	if len(m.messages) == 0 {
		hint := fsEmptyHintStyle().Render("尚无消息。在下方输入框输入消息后按 Enter 发送。")
		// Pad to fill the viewport so the input panel stays anchored at the
		// bottom of the screen.
		return padToHeight(hint, m.viewport.ViewportHeight)
	}

	// Build height array
	heights := make([]int, len(m.messages))
	for i, msg := range m.messages {
		if msg.Rendered == "" {
			m.renderMessage(msg)
		}
		heights[i] = msg.Height
	}

	// Calculate visible range
	startIdx, endIdx := m.viewport.VisibleRange(heights)

	var b strings.Builder

	// Render visible messages
	for i := startIdx; i < endIdx; i++ {
		msg := m.messages[i]
		b.WriteString(msg.Rendered)
		b.WriteString("\n")
	}

	return padToHeight(b.String(), m.viewport.ViewportHeight)
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

// renderInputArea renders the floating input panel and help hint.
func (m *FullscreenModel) renderInputArea() string {
	var b strings.Builder

	// Floating rounded-border input panel containing the textarea.
	panel := fsInputPanelStyle(m.termWidth).Render(m.textarea.View())
	b.WriteString(panel)
	b.WriteString("\n")

	// Help hint along the bottom.
	help := fsHelpStyle().Render(
		"Enter 发送 · Shift+Enter 换行 · PgUp/PgDn 翻页 · Home/End 顶/底 · 鼠标滚轮 滚动 · Ctrl+C 退出")
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

	// Decorate tool messages with a small status badge for the role label.
	if msg.Role == "tool" {
		// Tool messages use ToolName + status on the first line, result on
		// subsequent lines; handleToolUseMessage already encodes that.
		// Preserve the existing first-line summary as the label tail.
	}

	header := fsRoleLabelStyle(borderColor).Render(label)

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
	m.messages = append(m.messages, msg)
	m.updateViewportHeight()
}

// handleIncomingMessage handles incoming messages from the adapter.
func (m *FullscreenModel) handleIncomingMessage(msg Message) {
	if msg.Role == "assistant_stream" {
		// Streaming content - accumulate in buffer
		if !m.isStreaming {
			m.isStreaming = true
			m.streamingBuf.Reset()
		}
		m.streamingBuf.WriteString(msg.Content)

		// Update or create streaming message
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant_stream" {
			m.messages[len(m.messages)-1].Content = m.streamingBuf.String()
			m.renderMessage(m.messages[len(m.messages)-1])
		} else {
			newMsg := NewFormattedMessage(fmt.Sprintf("assistant-%d", time.Now().UnixNano()), "assistant_stream", m.streamingBuf.String())
			m.renderMessage(newMsg)
			m.messages = append(m.messages, newMsg)
		}
	} else {
		// Regular message
		m.isStreaming = false
		newMsg := NewFormattedMessage(fmt.Sprintf("%s-%d", msg.Role, time.Now().UnixNano()), msg.Role, msg.Content)
		m.renderMessage(newMsg)
		m.messages = append(m.messages, newMsg)
	}

	m.updateViewportHeight()
}

// handleThinkingMessage handles thinking messages.
func (m *FullscreenModel) handleThinkingMessage(msg ThinkingMsg) {
	content := msg.Title
	if msg.Reasoning != "" {
		content += "\n" + msg.Reasoning
	}
	newMsg := NewFormattedMessage(fmt.Sprintf("thinking-%d", time.Now().UnixNano()), "thinking", content)
	m.renderMessage(newMsg)
	m.messages = append(m.messages, newMsg)
	m.updateViewportHeight()
}

// handleToolUseMessage handles tool use messages.
func (m *FullscreenModel) handleToolUseMessage(msg ToolUseMsg) {
	content := fmt.Sprintf("%s [%s]", msg.ToolName, msg.Status)
	if msg.Result != "" {
		content += "\n" + msg.Result
	}
	newMsg := NewFormattedMessage(msg.ID, "tool", content)
	m.renderMessage(newMsg)
	m.messages = append(m.messages, newMsg)
	m.updateViewportHeight()
}

// updateViewportHeight recalculates total height and updates viewport.
func (m *FullscreenModel) updateViewportHeight() {
	totalHeight := 0
	for _, msg := range m.messages {
		totalHeight += msg.Height
	}
	m.viewport.SetTotalHeight(totalHeight)
}

// BindOutbound binds the outbound message handler.
func (m *FullscreenModel) BindOutbound(fn func(eventsource.Outbound) error) {
	m.outbound = fn
}
