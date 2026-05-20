package cli

import (
	"bytes"
	"errors"
	"syscall"
	"testing"
)

// mockWriter is a test writer that can simulate EPIPE errors
type mockWriter struct {
	buf       bytes.Buffer
	shouldErr bool
	err       error
}

func (m *mockWriter) Write(p []byte) (int, error) {
	if m.shouldErr {
		return 0, m.err
	}
	return m.buf.Write(p)
}

func TestSafeWrite(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		err       error
		wantErr   bool
		wantNil   bool
		wantBytes int
	}{
		{
			name:      "successful write",
			data:      []byte("hello"),
			err:       nil,
			wantErr:   false,
			wantNil:   false,
			wantBytes: 5,
		},
		{
			name:    "EPIPE error - should be gracefully ignored",
			data:    []byte("hello"),
			err:     syscall.EPIPE,
			wantErr: false,
			wantNil: true,
		},
		{
			name:    "other error - should propagate",
			data:    []byte("hello"),
			err:     errors.New("generic error"),
			wantErr: true,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := &mockWriter{
				shouldErr: tt.err != nil,
				err:       tt.err,
			}

			err := SafeWrite(mw, tt.data)

			if tt.wantErr && err == nil {
				t.Errorf("SafeWrite() expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("SafeWrite() unexpected error: %v", err)
			}
			if tt.wantNil && err != nil {
				t.Errorf("SafeWrite() expected nil for EPIPE but got: %v", err)
			}
			if !tt.wantErr && !tt.wantNil && mw.buf.Len() != tt.wantBytes {
				t.Errorf("SafeWrite() wrote %d bytes, want %d", mw.buf.Len(), tt.wantBytes)
			}
		})
	}
}

func TestIsPipedStdin(t *testing.T) {
	// Note: This test depends on the environment it runs in.
	// When running in a CI environment, stdin may be piped.
	// We just verify the function doesn't panic.
	_ = IsPipedStdin()
}

func TestIsPipedStdout(t *testing.T) {
	// Note: This test depends on the environment it runs in.
	// When running in a CI environment, stdout may be piped.
	// We just verify the function doesn't panic.
	_ = IsPipedStdout()
}

func TestIgnoreSIGPIPE(t *testing.T) {
	// Verify the function doesn't panic
	IgnoreSIGPIPE()
}
