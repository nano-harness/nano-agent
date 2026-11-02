package bubbletea

import (
	"fmt"
	"sort"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// dumpFormattedMessages renders the entire MessageStore as a plain-text
// blob suitable for emission via tea.Printf right before exiting the
// Bubble Tea program. The dump is intentionally full-fidelity: it
// reflects msg.Content rather than msg.Rendered so collapsed thinking
// blocks, truncated tool results, or any other on-screen summarisation
// does NOT cause information to be lost in the scrollback record.
//
// Each message is emitted as a role-labeled block separated by a blank
// line. Tool messages additionally expand the metadata fields
// (tool_name, tool_status, tool_params, tool_result) so the full
// arguments and result text are preserved.
func dumpFormattedMessages(store *MessageStore) string {
	if store == nil || store.Len() == 0 {
		return ""
	}
	var b strings.Builder
	store.Range(func(_ int, msg *FormattedMessage) bool {
		block := formatDumpMessage(msg)
		block = strings.TrimRight(block, "\n")
		if block == "" {
			return true
		}
		b.WriteString(block)
		b.WriteString("\n\n")
		return true
	})
	return strings.TrimRight(b.String(), "\n")
}

// formatDumpMessage builds the plain-text block for a single message,
// expanding tool metadata when present.
func formatDumpMessage(msg *FormattedMessage) string {
	if msg == nil {
		return ""
	}
	role := msg.Role
	if msg.Role == "tool" {
		if block, ok := formatToolDumpFromMetadata(msg); ok {
			return block
		}
	}
	content := strings.TrimRight(msg.Content, "\n")
	if content == "" {
		return ""
	}
	return fmt.Sprintf("[%s] %s", role, content)
}

// formatToolDumpFromMetadata renders a tool block from msg.Metadata
// fields when any of the structured fields are present. Returns
// (block, true) when at least one tool field is found; otherwise
// returns ("", false) so the caller falls back to the generic format.
func formatToolDumpFromMetadata(msg *FormattedMessage) (string, bool) {
	if msg == nil || msg.Metadata == nil {
		return "", false
	}
	name, hasName := metaString(msg.Metadata, "tool_name")
	status, hasStatus := metaString(msg.Metadata, "tool_status")
	params, hasParams := metaString(msg.Metadata, "tool_params")
	result, hasResult := metaString(msg.Metadata, "tool_result")
	if !hasName && !hasStatus && !hasParams && !hasResult {
		return "", false
	}
	var b strings.Builder
	header := "[tool]"
	if hasName {
		header += " " + name
	}
	if hasStatus {
		header += " (" + status + ")"
	}
	b.WriteString(header)
	if hasParams {
		b.WriteString("\nparams:\n")
		b.WriteString(params)
	}
	if hasResult {
		b.WriteString("\nresult:\n")
		b.WriteString(result)
	}
	return b.String(), true
}

// metaString returns the metadata value for key as a string. Strings
// are returned verbatim; non-string values are formatted with %v. The
// boolean result is false only when the key is absent.
func metaString(meta map[string]interface{}, key string) (string, bool) {
	v, ok := meta[key]
	if !ok {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case map[string]interface{}:
		// Render map params with stable key order so dumps are
		// deterministic — important for the regression tests.
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for i, k := range keys {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%s: %v", k, x[k])
		}
		return b.String(), true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

// DumpHistoryToScrollback renders the entire message history as a plain-text
// blob suitable for emission via tea.Printf right before exiting alt-screen.
// See dumpFormattedMessages for the rendering contract.
func (m *FullscreenModel) DumpHistoryToScrollback() string {
	if m == nil {
		return ""
	}
	return dumpFormattedMessages(m.messages)
}

// DumpHistoryPlainText returns the same dump as DumpHistoryToScrollback but
// strips ANSI escape codes. Useful in tests that want to assert on textual
// content without worrying about styling.
func (m *FullscreenModel) DumpHistoryPlainText() string {
	return xansi.Strip(m.DumpHistoryToScrollback())
}

// dumpHistory renders the inline (`--tea`) model's message history. Mirrors
// FullscreenModel.DumpHistoryToScrollback so Ctrl+C in inline mode leaves a
// faithful conversation transcript in the host terminal's scrollback.
func (m *Model) dumpHistory() string {
	if m == nil {
		return ""
	}
	return dumpFormattedMessages(m.messages)
}
