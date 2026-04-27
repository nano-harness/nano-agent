package banner

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// Options configures banner playback.
type Options struct {
	Theme       Theme
	Colorize    bool            // false degrades to static last frame
	MaxDuration time.Duration   // safety valve, default 3.5s
	SkipCh      <-chan struct{} // optional: external skip notification
}

// Play synchronously plays the banner, until completion / timeout / SkipCh.
// Render strategy: each frame uses ESC[H + ESC[2J to clear screen (only clears banner area).
func Play(w io.Writer, opts Options) error {
	frames, err := LoadFrames()
	if err != nil {
		return err
	}
	if opts.Theme == nil {
		opts.Theme = DefaultTheme
	}
	if opts.MaxDuration == 0 {
		opts.MaxDuration = 3500 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.MaxDuration)
	defer cancel()

	// Non-TTY or NO_COLOR: print only last frame static LOGO (no clear, no animation)
	if !opts.Colorize {
		if len(frames) > 0 {
			fmt.Fprintln(w, RenderFrame(frames[len(frames)-1], opts.Theme, false))
		}
		return nil
	}

	// Hide cursor
	fmt.Fprint(w, "\x1b[?25l")
	defer fmt.Fprint(w, "\x1b[?25h")

	for i, f := range frames {
		// After first frame, move cursor up N lines back to banner top, then clear to screen end
		if i > 0 {
			lines := countLines(frames[i-1].Content)
			fmt.Fprintf(w, "\x1b[%dA\x1b[J", lines)
		}
		fmt.Fprintln(w, RenderFrame(f, opts.Theme, true))

		select {
		case <-ctx.Done():
			return nil
		case <-opts.SkipCh:
			return nil
		case <-time.After(f.Duration):
		}
	}
	return nil
}

// IsInteractiveTTY determines if stdout is an interactive terminal, and NO_COLOR is not set.
func IsInteractiveTTY() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func countLines(s string) int {
	n := 1
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}
