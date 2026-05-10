package bubbletea

import "strings"

// HistorySearch implements an incremental, reverse-i-search style search
// over a slice of past inputs. It is the same model used by `bash`/`readline`
// when the user presses Ctrl+R: the most recent matching entry is selected
// first, and pressing Ctrl+R again cycles to the next-older match.
//
// The struct is deliberately decoupled from bubbletea so tests can drive the
// state machine without instantiating a full Model. The bubbletea event
// handler delegates to TypeRune / Backspace / Next on Ctrl+R / printable
// keys / Backspace, and queries Active / Query / Selected when rendering.
type HistorySearch struct {
	history []string
	active  bool
	query   string
	// matches caches indices into history that contain query (case-insensitive).
	// It is rebuilt whenever query changes. Order is most-recent-first.
	matches []int
	// cursor is the index into matches of the currently selected entry.
	cursor int
}

// NewHistorySearch constructs a state machine that searches the given
// history slice (oldest first; the same format used by Model.inputHistory).
// The slice is captured by reference; the caller must not mutate it during
// search.
func NewHistorySearch(history []string) *HistorySearch {
	return &HistorySearch{history: history}
}

// Active reports whether reverse search mode is currently engaged.
func (h *HistorySearch) Active() bool { return h.active }

// Query returns the substring currently being searched for.
func (h *HistorySearch) Query() string { return h.query }

// Selected returns the entry currently selected (most recent match for the
// current query, after `Next` advances). Returns "" when no match exists.
func (h *HistorySearch) Selected() string {
	if !h.active || len(h.matches) == 0 {
		return ""
	}
	if h.cursor < 0 || h.cursor >= len(h.matches) {
		return ""
	}
	return h.history[h.matches[h.cursor]]
}

// Begin enters reverse search mode. Calling it again while already active
// is a no-op so the caller can bind it directly to Ctrl+R without tracking
// state externally.
func (h *HistorySearch) Begin() {
	if h.active {
		return
	}
	h.active = true
	h.query = ""
	h.matches = nil
	h.cursor = 0
}

// End exits reverse search mode and resets internal state.
func (h *HistorySearch) End() {
	h.active = false
	h.query = ""
	h.matches = nil
	h.cursor = 0
}

// TypeRune appends a rune to the query and recomputes matches. The cursor
// is reset to the most-recent match so each new character anchors back to
// the freshest entry, matching readline's behavior.
func (h *HistorySearch) TypeRune(r rune) {
	if !h.active {
		return
	}
	h.query += string(r)
	h.recomputeMatches()
}

// Backspace removes the last character of the query. If the query becomes
// empty the matches list is cleared but the search stays active; pressing
// Backspace again is a no-op until a new character is typed or End is called.
func (h *HistorySearch) Backspace() {
	if !h.active || h.query == "" {
		return
	}
	runes := []rune(h.query)
	h.query = string(runes[:len(runes)-1])
	h.recomputeMatches()
}

// Next advances the cursor to the next-older match. If the cursor is
// already on the oldest match the call is a no-op (i.e. it does not wrap).
// Wrapping would surprise users who expect Ctrl+R to monotonically walk
// back through history.
func (h *HistorySearch) Next() {
	if !h.active || len(h.matches) == 0 {
		return
	}
	if h.cursor+1 < len(h.matches) {
		h.cursor++
	}
}

// recomputeMatches rebuilds the matches slice. Matching is case-insensitive
// substring containment. History is scanned newest-first so the resulting
// matches slice already has the correct ordering for `Next`.
func (h *HistorySearch) recomputeMatches() {
	h.matches = h.matches[:0]
	h.cursor = 0
	if h.query == "" {
		return
	}
	lq := strings.ToLower(h.query)
	for i := len(h.history) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(h.history[i]), lq) {
			h.matches = append(h.matches, i)
		}
	}
}
