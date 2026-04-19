package tview

import "fmt"

// FormatNumber formats large numbers with appropriate suffixes (K, M, B)
func FormatNumber(num int) string {
	if num < 1000 {
		return fmt.Sprintf("%d", num)
	} else if num < 1000000 {
		// Check if it's close to 1M (999K+ becomes 1.0M)
		if num >= 999500 { // 999.5K rounds to 1000.0K, so show as 1.0M
			return fmt.Sprintf("%.1fM", float64(num)/1000000.0)
		}
		return fmt.Sprintf("%.1fK", float64(num)/1000.0)
	} else if num < 1000000000 {
		// Check if it's close to 1B (999M+ becomes 1.0B)
		if num >= 999500000 { // 999.5M rounds to 1000.0M, so show as 1.0B
			return fmt.Sprintf("%.1fB", float64(num)/1000000000.0)
		}
		return fmt.Sprintf("%.1fM", float64(num)/1000000.0)
	}
	return fmt.Sprintf("%.1fB", float64(num)/1000000000.0)
}
