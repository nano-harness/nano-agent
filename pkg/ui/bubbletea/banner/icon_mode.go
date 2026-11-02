package banner

import (
	"strconv"
	"strings"
)

// IconMode selects which ASCII icon is displayed in the settled banner region.
type IconMode string

const (
	IconDefault IconMode = ""        // default: atom symbol ⚛
	IconTea     IconMode = "tea"     // classic teacup with steam
	IconMilkTea IconMode = "milktea" // bubble tea cup with straw
)

// teaLines are the 3 replacement lines for --tea mode (steam, cup body, base).
var teaLines = [3]string{
	"                                ~ ( ~",
	"                               |_____|)",
	"                                \\___/",
}

// milkTeaLines are the 3 replacement lines for --milktea mode (dome lid, cup body, base).
var milkTeaLines = [3]string{
	"                                .=|=.",
	"                                |o.o|",
	"                                 \\_/",
}

// iconLineRoles assigns semantic color roles to each of the 3 replacement lines:
// top (steam / lid) → shine, middle (cup body) → head, bottom (base) → trail.
var iconLineRoles = [3]AnimationElement{ElemShine, ElemHead, ElemTrail}

// isSettledFrame returns true when the frame shows the atom in its settled position:
// both ⚛ and ∘ are visible simultaneously (flying frames only have ⚛).
func isSettledFrame(f Frame) bool {
	return strings.Contains(f.Content, "⚛") && strings.Contains(f.Content, "∘")
}

// applyIconMode replaces the 3-line icon area (∘ decoration, ⚛ atom, ∘ decoration)
// with ASCII cup art in every settled frame. Flying frames are left unchanged.
func applyIconMode(frames []Frame, mode IconMode) {
	if mode == IconDefault {
		return
	}
	var newLines [3]string
	switch mode {
	case IconTea:
		newLines = teaLines
	case IconMilkTea:
		newLines = milkTeaLines
	default:
		return
	}

	for i := range frames {
		if !isSettledFrame(frames[i]) {
			continue
		}
		lines := strings.Split(frames[i].Content, "\n")
		atomRow := -1
		for r, line := range lines {
			if strings.Contains(line, "⚛") {
				atomRow = r
				break
			}
		}
		if atomRow < 1 || atomRow+1 >= len(lines) {
			continue
		}

		// Replace the 3 lines surrounding the atom (above, atom row, below).
		lines[atomRow-1] = newLines[0]
		lines[atomRow] = newLines[1]
		lines[atomRow+1] = newLines[2]
		frames[i].Content = strings.Join(lines, "\n")

		// Update color map: remove old entries for the 3 rows, add new ones.
		if frames[i].Colors == nil {
			frames[i].Colors = make(map[string]AnimationElement)
		}
		for _, row := range []int{atomRow - 1, atomRow, atomRow + 1} {
			prefix := strconv.Itoa(row) + ","
			for key := range frames[i].Colors {
				if strings.HasPrefix(key, prefix) {
					delete(frames[i].Colors, key)
				}
			}
		}
		for idx, row := range []int{atomRow - 1, atomRow, atomRow + 1} {
			role := iconLineRoles[idx]
			for col, r := range newLines[idx] {
				if r != ' ' {
					frames[i].Colors[fmtKey(row, col)] = role
				}
			}
		}
	}
}
