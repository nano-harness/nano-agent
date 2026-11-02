package event

import (
	"encoding/json"
	"testing"
)

func TestTokenStats_ContextWindowJSON(t *testing.T) {
	stats := TokenStats{
		InputTokens:       10,
		OutputTokens:      5,
		TotalTokens:       15,
		ContextWindowMax:  100,
		ContextUsedTokens: 42,
	}
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["context_window_max"] != 100 || got["context_used_tokens"] != 42 {
		t.Fatalf("context fields not serialized: %s", data)
	}
}
