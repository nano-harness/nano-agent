package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// tailFollow streams the contents of the log file at path to w. It first
// emits the last `lines` lines, then continues to poll the file for new
// content until ctx is cancelled. The implementation uses polling instead
// of inotify/fsevents to avoid adding new third-party dependencies and to
// keep behavior consistent across platforms.
//
// The tailer is robust against logrotate-style rotation: when the file
// shrinks (truncated in place) or the inode changes (renamed/replaced), it
// re-opens the file and resumes tailing from the beginning.
func tailFollow(ctx context.Context, path string, lines int, w io.Writer) error {
	if path == "" {
		return fmt.Errorf("log file not configured")
	}

	// Print the trailing window first so the user sees recent context.
	if lines > 0 {
		if err := printTailLines(path, lines, w); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	pollInterval := tailPollInterval
	var (
		file       *os.File
		curIno     uint64
		offset     int64
		readBuf    = make([]byte, 32*1024)
		openTicker = time.NewTicker(pollInterval)
	)
	defer openTicker.Stop()
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()

	openOrReopen := func(initial bool) error {
		if file != nil {
			_ = file.Close()
			file = nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		file = f
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		curIno = inodeOf(fi)
		if initial {
			// Skip past content we've already shown via printTailLines.
			offset = fi.Size()
			if _, err := file.Seek(offset, io.SeekStart); err != nil {
				return err
			}
		} else {
			offset = 0
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return err
			}
		}
		return nil
	}

	if err := openOrReopen(true); err != nil && !os.IsNotExist(err) {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-openTicker.C:
		}

		// If the file did not exist initially, keep retrying until it appears.
		if file == nil {
			if err := openOrReopen(false); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
		}

		fi, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				// File was rotated/deleted; close and wait for it to reappear.
				if file != nil {
					_ = file.Close()
					file = nil
				}
				continue
			}
			return err
		}

		// Detect rotation: inode change or shrunk file.
		if inodeOf(fi) != curIno || fi.Size() < offset {
			if err := openOrReopen(false); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			fi, err = os.Stat(path)
			if err != nil {
				continue
			}
		}

		if fi.Size() == offset {
			continue
		}

		for {
			n, err := file.Read(readBuf)
			if n > 0 {
				if _, werr := w.Write(readBuf[:n]); werr != nil {
					return werr
				}
				offset += int64(n)
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
	}
}

// tailPollInterval is the polling cadence used by tailFollow. It is a
// package-level variable so tests can override it for fast iteration.
var tailPollInterval = 200 * time.Millisecond

// printTailLines writes the last n lines of the file at path to w. It uses
// a streaming approach so very large log files do not need to be loaded
// fully into memory.
func printTailLines(path string, n int, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if n <= 0 {
		n = 50
	}
	// Circular ring buffer: once filled, each incoming line overwrites the
	// oldest slot in O(1) time instead of shifting the slice.
	ring := make([]string, n)
	count := 0
	head := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if count < n {
			ring[count] = line
			count++
			continue
		}
		ring[head] = line
		head = (head + 1) % n
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		idx := i
		if count == n {
			idx = (head + i) % n
		}
		line := ring[idx]
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		if _, err := w.Write([]byte(line)); err != nil {
			return err
		}
	}
	return nil
}
