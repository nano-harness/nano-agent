package bubbletea

import "testing"

func TestCronStatusMsgIsExportedThinMessage(t *testing.T) {
	msg := CronStatusMsg{Indicator: "⏰ 1 running"}
	if msg.Indicator != "⏰ 1 running" {
		t.Fatalf("Indicator = %q", msg.Indicator)
	}
}
