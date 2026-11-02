package banner

import (
	"strconv"
)

// Theme maps semantic roles to 256-color ANSI codes.
// Reuses semantic color conventions from model.go to maintain visual consistency.
type Theme map[AnimationElement]string

// DefaultTheme is suitable for dark terminal backgrounds, aligning with model.go colorXxx:
//
//	border  -> colorMuted   "245"
//	head    -> colorUser    "75"   (soft blue)
//	eyes    -> colorSuccess "114"  (soft green)
//	goggles -> colorStatus  "73"   (soft teal)
//	shine   -> colorBright  "255"
//	stars   -> colorWarning "215"
//	text    -> colorAssistant "115" (sage green)
//	block_shadow -> colorSecondary "248"
//	trail   -> colorOpenSpec "135" (soft purple)
var DefaultTheme = Theme{
	ElemBorder:    "245",
	ElemHead:      "75",
	ElemEyes:      "114",
	ElemGoggles:   "73",
	ElemShine:     "255",
	ElemStars:     "215",
	ElemText:      "75",
	ElemTextShade: "248",
	ElemTrail:     "135",
	ElemSubtitle:  "249",
}

// ColorFor returns the ANSI color code for the given coordinate (row, col) in the specified frame;
// returns empty string if no mapping (uses terminal default color).
func (t Theme) ColorFor(row, col int, f Frame) string {
	if f.Colors == nil {
		return ""
	}
	key := fmtKey(row, col)
	if elem, ok := f.Colors[key]; ok {
		if c, ok2 := t[elem]; ok2 {
			return c
		}
	}
	return ""
}

func fmtKey(row, col int) string {
	return strconv.Itoa(row) + "," + strconv.Itoa(col)
}
