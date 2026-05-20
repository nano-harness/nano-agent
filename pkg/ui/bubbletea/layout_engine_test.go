package bubbletea

import "testing"

func TestLayoutEngine_ModeBoundaries(t *testing.T) {
	cases := []struct {
		w    int
		want LayoutMode
	}{
		{120, LayoutWide},
		{100, LayoutWide},
		{99, LayoutNormal},
		{80, LayoutNormal},
		{60, LayoutNormal},
		{59, LayoutNarrow},
		{40, LayoutNarrow},
		{39, LayoutMinimal},
		{20, LayoutMinimal},
		{0, LayoutMinimal},
	}
	l := NewLayoutEngine()
	for _, c := range cases {
		l.Update(c.w, 24)
		if got := l.Mode(); got != c.want {
			t.Fatalf("width=%d -> mode=%d, want %d", c.w, got, c.want)
		}
	}
}

func TestLayoutEngine_HeightsSumToTermHeight(t *testing.T) {
	l := NewLayoutEngine()
	if got := l.StatusBarHeight(); got != 2 {
		t.Fatalf("StatusBarHeight()=%d, want 2", got)
	}
	for _, w := range []int{120, 80, 50, 30} {
		for _, h := range []int{40, 24, 10, 5} {
			l.Update(w, h)
			total := l.StatusBarHeight() + l.InputHeight() + l.HelpHeight() + l.MessageAreaHeight()
			if h >= l.StatusBarHeight()+l.InputHeight()+l.HelpHeight() {
				if total != h {
					t.Fatalf("w=%d h=%d: heights sum=%d, want %d", w, h, total, h)
				}
			}
			if l.MessageAreaHeight() < 0 {
				t.Fatalf("w=%d h=%d: negative message area height", w, h)
			}
		}
	}
}

func TestLayoutEngine_ContentWidthMinimum(t *testing.T) {
	l := NewLayoutEngine()
	l.Update(5, 24)
	if l.ContentWidth() < 10 {
		t.Fatalf("ContentWidth must clamp to >=10, got %d", l.ContentWidth())
	}
	if l.InputInnerWidth() < 10 {
		t.Fatalf("InputInnerWidth must clamp to >=10, got %d", l.InputInnerWidth())
	}
}

func TestLayoutEngine_MinimalSkipsBorder(t *testing.T) {
	l := NewLayoutEngine()
	l.Update(30, 24)
	if l.ShouldUseBorder() {
		t.Fatal("minimal layout must not request a border")
	}
	l.Update(80, 24)
	if !l.ShouldUseBorder() {
		t.Fatal("normal layout should request a border")
	}
}

func TestLayoutEngine_HandlesNegativeDimensions(t *testing.T) {
	l := NewLayoutEngine()
	l.Update(-5, -5)
	if l.TermWidth() != 0 || l.TermHeight() != 0 {
		t.Fatalf("negative dimensions must clamp to 0, got %dx%d", l.TermWidth(), l.TermHeight())
	}
	if l.MessageAreaHeight() < 0 {
		t.Fatal("message area must never be negative")
	}
}
