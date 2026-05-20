package bubbletea

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/nano-harness/nano-agent/pkg/slash"
	xansi "github.com/charmbracelet/x/ansi"
)

// openCommandsPalette loads the slash command registry and switches the
// fullscreen view into command-palette mode. The palette mirrors the
// behavior of the inline (--tea) palette so users get the same
// browseable, mouseable command list in milktea (--milktea).
func (m *FullscreenModel) openCommandsPalette() {
	m.loadCommands()
	m.showingCommands = true
	if m.commandsIndex >= len(m.commands) {
		m.commandsIndex = 0
	}
	m.commandsScrollOffset = 0
}

// loadCommands populates m.commands and m.slashNames from the slash
// registry rooted at the model's working directory. It is safe to call
// multiple times; each call refreshes the cached lists.
func (m *FullscreenModel) loadCommands() {
	cwd := m.cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	reg := slash.NewRegistry(cwd)
	m.commands = reg.All()
	m.slashNames = reg.Names()
}

// moveCommandSelection moves the highlight by `delta` (positive = down)
// and keeps the selection within the visible scroll window.
func (m *FullscreenModel) moveCommandSelection(delta int) {
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

// hitTestCommandItem maps a click coordinate to a command index in the
// currently rendered palette window. Returns -1 when no row matches.
func (m *FullscreenModel) hitTestCommandItem(x, y int) int {
	for _, box := range m.commandItems {
		if y == box.y && x >= box.x0 && (box.x1 == 0 || x < box.x1) {
			return box.index
		}
	}
	return -1
}

// insertSelectedCommand writes "/<name> " into the input textarea so the
// user can supply arguments before submitting. Mirrors the inline mode.
func (m *FullscreenModel) insertSelectedCommand() {
	if len(m.commands) == 0 {
		return
	}
	name := m.commands[m.commandsIndex].Name
	m.textarea.SetValue("/" + name + " ")
	m.textarea.CursorEnd()
}

// handleCommandPaletteKey dispatches keys while the palette is visible.
// Returns the resulting tea.Cmd (always nil in practice — the palette is
// purely interactive UI state).
func (m *FullscreenModel) handleCommandPaletteKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		m.moveCommandSelection(-1)
	case "down", "j":
		m.moveCommandSelection(1)
	case "pgup":
		m.moveCommandSelection(-commandsPaletteVisibleRows)
	case "pgdown":
		m.moveCommandSelection(commandsPaletteVisibleRows)
	case "home":
		m.moveCommandSelection(-len(m.commands))
	case "end":
		m.moveCommandSelection(len(m.commands))
	case "enter":
		m.insertSelectedCommand()
		m.showingCommands = false
	case "esc", "q", "ctrl+c":
		m.showingCommands = false
	}
	return nil
}

// renderCommandsPalette writes a categorized, scrollable command list and
// a detail block for the currently selected command. Visual structure is
// kept compatible with the inline palette so the user experience is
// identical between --tea and --milktea.
func (m *FullscreenModel) renderCommandsPalette(b *strings.Builder) {
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
	// Compute the screen-row offset where the title is rendered so
	// hit-test coordinates line up with the actual View() output. The
	// palette is prefixed by the status bar (1 row) and a separator
	// newline (1 row), so the title sits at row 2.
	const titleRow = 2
	b.WriteString(title + "\n\n")

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

	currentCat := slash.Category("")
	renderedRows := 0
	startIdx := m.commandsScrollOffset
	// `row` tracks the absolute screen row of the next line we are about
	// to write so mouse hit-tests can compare directly against y.
	row := titleRow + 2 // title + blank line after title

	for i := startIdx; i < total && renderedRows < visibleRows; i++ {
		it := m.commands[i]

		if it.Category != currentCat {
			currentCat = it.Category
			label := categoryLabels[currentCat]
			color := categoryColors[currentCat]
			catStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))
			b.WriteString("\n" + catStyle.Render("── "+label+" ──") + "\n")
			renderedRows += 2
			row += 2
			if renderedRows >= visibleRows {
				break
			}
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
		help := lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted)).Render("Enter 插入 | ↑↓ 选择 | Esc 返回")
		box := lipgloss.NewStyle().Padding(1, 2).Render(pb.String())
		b.WriteString(box + "\n" + help + "\n")
	}
}

// uniquePrefixMatch returns the single name in `names` that has `prefix`
// as a strict prefix, or "" when zero or more than one match. Names that
// equal the prefix exactly are returned as-is to preserve idempotence of
// repeated Tab presses.
func uniquePrefixMatch(names []string, prefix string) string {
	matches := allPrefixMatches(names, prefix)
	if len(matches) != 1 {
		return ""
	}
	return matches[0]
}

// allPrefixMatches returns every name with the given prefix, in
// registry order. Returns nil for an empty prefix to avoid offering the
// entire registry as a "completion".
func allPrefixMatches(names []string, prefix string) []string {
	if prefix == "" {
		return nil
	}
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}
