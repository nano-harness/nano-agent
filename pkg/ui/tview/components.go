package tview

import (
	"regexp"

	"github.com/atotto/clipboard"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Component represents a TUI component interface
type Component interface {
	GetPrimitive() tview.Primitive
	Update()
	SetFocus(bool)
	HandleInput(*tcell.EventKey) bool
}

// ChatView represents the chat display component with selection support
// Use tview.TextArea under the hood to enable native text selection and copy
// while behaving read-only for normal interactions.
type ChatView struct {
	area        *tview.TextArea
	container   tview.Primitive
	currentText string
}

// NewChatView creates a new chat view component
func NewChatView() *ChatView {
	// Text area for real selection support (configured as read-only)
	textArea := tview.NewTextArea()
	textArea.
		SetWrap(true).
		SetBorder(true).
		SetTitle(" 💬 Chat (点击聊天区域后按 Ctrl+C 复制所选内容, Ctrl+A 复制全部) ").
		SetTitleAlign(tview.AlignLeft)

	// Integrate with OS clipboard
	textArea.SetClipboard(
		func(text string) {
			// copy
			_ = clipboard.WriteAll(text)
		},
		func() string {
			// paste
			clip, _ := clipboard.ReadAll()
			return clip
		},
	)

	cv := &ChatView{
		area:      textArea,
		container: textArea, // container is the area itself
	}

	cv.setupReadOnlySelection()

	return cv
}

// GetPrimitive returns the underlying tview primitive
func (cv *ChatView) GetPrimitive() tview.Primitive {
	return cv.container
}

// Update refreshes the chat view display
func (cv *ChatView) Update() {
	// Empty implementation - actual updates handled by Model
}

// SetFocus sets focus state for the component
func (cv *ChatView) SetFocus(_ bool) {
	// Chat view doesn't need special focus handling
}

// HandleInput handles key input for the chat view
func (cv *ChatView) HandleInput(_ *tcell.EventKey) bool {
	return false // Let Model handle input
}

// setupReadOnlySelection configures read-only behavior with selection/copy helpers
func (cv *ChatView) setupReadOnlySelection() {
	cv.area.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			// Let the global handler deal with Ctrl+C to avoid conflicts
			// The global handler will copy the chat content when chat view has focus
			return event
		case tcell.KeyCtrlA:
			// Let the global handler deal with Ctrl+A to ensure consistency
			// The global handler will copy the chat content regardless of focus
			return event
		case tcell.KeyRune:
			// Prevent typing in read-only view
			return nil
		case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyDelete, tcell.KeyCtrlV, tcell.KeyCtrlX:
			// Block editing keys
			return nil
		}
		return event
	})
}

var tviewTagRegexp = regexp.MustCompile(`\[[^\[\]]+\]`)

// stripTviewTags removes tview color/style tags like [green], [-], [::b], etc.
func stripTviewTags(s string) string {
	return tviewTagRegexp.ReplaceAllString(s, "")
}

// SetText sets the text content (formatted with tview tags). It will be
// converted to plain text for display in the read-only TextArea.
func (cv *ChatView) SetText(text string) {
	cv.currentText = stripTviewTags(text)
	cv.area.SetText(cv.currentText, true)
}

// ScrollToEnd moves the cursor to the end so the latest content is visible.
func (cv *ChatView) ScrollToEnd() {
	// Setting the same text with cursorAtTheEnd=true ensures we are at the end.
	cv.area.SetText(cv.currentText, true)
}

// SetMouseCapture forwards mouse capture to the underlying primitive.
func (cv *ChatView) SetMouseCapture(capture func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse)) {
	// Forward directly to the TextArea (inherits Box)
	cv.area.SetMouseCapture(capture)
}

// ScrollBy scrolls the view by delta lines using cursor movement to keep it visible.
func (cv *ChatView) ScrollBy(delta int) {
	if delta == 0 {
		return
	}
	h := cv.area.InputHandler()
	if h == nil {
		return
	}
	key := tcell.KeyDown
	if delta < 0 {
		key = tcell.KeyUp
		delta = -delta
	}
	for i := 0; i < delta; i++ {
		h(tcell.NewEventKey(key, 0, tcell.ModNone), func(_ tview.Primitive) {})
	}
}

// StatusBar represents the status bar component
type StatusBar struct {
	*tview.TextView
}

// NewStatusBar creates a new status bar component
func NewStatusBar() *StatusBar {
	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(false).
		SetWordWrap(false)

	textView.SetBorder(false)

	sb := &StatusBar{
		TextView: textView,
	}

	return sb
}

// GetPrimitive returns the underlying tview primitive
func (sb *StatusBar) GetPrimitive() tview.Primitive {
	return sb.TextView
}

// InputField represents the input field component using TextArea for multi-line support
type InputField struct {
	*tview.TextArea
	// placeholder mirrors the current placeholder because tview.TextArea does not expose a getter.
	placeholder string
}

// NewInputField creates a new input field component with multi-line support
func NewInputField() *InputField {
	input := tview.NewTextArea()
	input.SetBorder(true)
	input.SetTitle(" ➤ 多行输入 (Enter换行, 双击Enter发送) ")
	input.SetTitleAlign(tview.AlignLeft)
	input.SetWrap(true) // Enable word wrapping
	inputField := &InputField{TextArea: input}
	// Set placeholder text
	inputField.SetPlaceholder("支持多行输入和粘贴，Enter换行，双击Enter发送消息")

	// Integrate with OS clipboard for copy/paste support
	input.SetClipboard(
		func(text string) {
			// copy
			_ = clipboard.WriteAll(text)
		},
		func() string {
			// paste
			clip, _ := clipboard.ReadAll()
			return clip
		},
	)

	return inputField
}

// GetPrimitive returns the underlying tview primitive
func (i *InputField) GetPrimitive() tview.Primitive {
	return i.TextArea
}

// GetText returns the current text content
func (i *InputField) GetText() string {
	return i.TextArea.GetText()
}

// SetText sets the text content
func (i *InputField) SetText(text string) {
	i.TextArea.SetText(text, true)
}

// SetPlaceholder sets the placeholder text.
func (i *InputField) SetPlaceholder(placeholder string) *InputField {
	i.placeholder = placeholder
	i.TextArea.SetPlaceholder(placeholder)
	return i
}

// GetPlaceholder returns the current placeholder text.
func (i *InputField) GetPlaceholder() string {
	return i.placeholder
}

// SetDisabled sets the disabled state
func (i *InputField) SetDisabled(disabled bool) {
	i.TextArea.SetDisabled(disabled)
}

// IsDisabled returns whether the input field is disabled.
func (i *InputField) IsDisabled() bool {
	return i.TextArea.GetDisabled()
}

// SetTextColor sets the text color
func (i *InputField) SetTextColor(color tcell.Color) {
	i.SetTextStyle(tcell.StyleDefault.Foreground(color))
}

// SetBackgroundColor sets the background color
func (i *InputField) SetBackgroundColor(color tcell.Color) {
	i.TextArea.SetBackgroundColor(color)
}

// SetBorderColor sets the border color
func (i *InputField) SetBorderColor(color tcell.Color) {
	i.TextArea.SetBorderColor(color)
}

// SetInputCapture sets the input capture function
func (i *InputField) SetInputCapture(capture func(event *tcell.EventKey) *tcell.EventKey) {
	i.TextArea.SetInputCapture(capture)
}
