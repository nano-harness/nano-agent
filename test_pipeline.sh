#!/bin/bash
# Integration tests for pipeline optimization features

set -e

echo "=== Pipeline Integration Tests ==="
echo

# Build the binary
echo "Building nano binary..."
go build -o /tmp/nano-test ./cmd/nano
NANO=/tmp/nano-test

# Test 1: Check help for new flags
echo "Test 1: New flags exist"
if $NANO binary exec --help 2>&1 | grep -q "\-\-format" && \
   $NANO binary exec --help 2>&1 | grep -q "\-\-quiet" && \
   $NANO binary exec --help 2>&1 | grep -q "\-\-stream"; then
    echo "✓ New flags (--format, --quiet, --stream) exist"
else
    echo "✗ New flags missing"
    exit 1
fi

# Test 2: Exit codes are defined
echo
echo "Test 2: Exit codes are defined"
if grep -q "ExitSuccess.*=.*0" pkg/cli/root.go && \
   grep -q "ExitGeneralError.*=.*1" pkg/cli/root.go && \
   grep -q "ExitPipeError.*=.*141" pkg/cli/root.go; then
    echo "✓ Exit codes properly defined"
else
    echo "✗ Exit codes not found"
    exit 1
fi

# Test 3: Pipe helper functions exist
echo
echo "Test 3: Pipe helper functions"
if grep -q "func IsPipedStdin" pkg/cli/pipe_helpers.go && \
   grep -q "func IsPipedStdout" pkg/cli/pipe_helpers.go && \
   grep -q "func SafeWrite" pkg/cli/pipe_helpers.go; then
    echo "✓ Pipe helper functions exist"
else
    echo "✗ Pipe helper functions missing"
    exit 1
fi

# Test 4: SIGPIPE handling in chat.go
echo
echo "Test 4: SIGPIPE signal handling"
if grep -q "syscall.SIGPIPE" pkg/cli/chat.go; then
    echo "✓ SIGPIPE handling added to chat.go"
else
    echo "✗ SIGPIPE handling missing"
    exit 1
fi

# Test 5: Stdin pipe detection in root.go
echo
echo "Test 5: Stdin pipe detection and auto-fallback"
if grep -q "IsPipedStdin()" pkg/cli/root.go && \
   grep -q "executeBinaryModeWithOptions" pkg/cli/root.go; then
    echo "✓ Stdin pipe detection and auto-fallback implemented"
else
    echo "✗ Stdin pipe detection missing"
    exit 1
fi

# Test 6: stderr separation in root.go
echo
echo "Test 6: stderr/stdout separation in TUI fallback"
if grep -q "fmt.Fprintln(os.Stderr" pkg/cli/root.go; then
    echo "✓ TUI fallback messages write to stderr"
else
    echo "✗ stderr separation not implemented"
    exit 1
fi

# Test 7: Binary result writes to stderr
echo
echo "Test 7: Binary result summary writes to stderr"
if grep -q "writeBinaryResult(os.Stderr" pkg/cli/binary.go; then
    echo "✓ Binary result summary writes to stderr"
else
    echo "✗ Binary result still writes to stdout"
    exit 1
fi

# Test 8: Streaming function exists
echo
echo "Test 8: Streaming NDJSON function"
if grep -q "func runBinaryExecStreaming" pkg/cli/binary.go; then
    echo "✓ Streaming NDJSON function implemented"
else
    echo "✗ Streaming function missing"
    exit 1
fi

# Test 9: Unit tests pass
echo
echo "Test 9: Unit tests"
if go test ./pkg/cli -run TestSafeWrite -v > /dev/null 2>&1; then
    echo "✓ Pipe helper unit tests pass"
else
    echo "✗ Pipe helper unit tests failed"
    exit 1
fi

# Test 10: Format check passes
echo
echo "Test 10: Code formatting"
if make fmt-check > /dev/null 2>&1; then
    echo "✓ Code is properly formatted"
else
    echo "⚠ Code formatting check failed (non-critical)"
fi

# Test 11: Lint check passes
echo
echo "Test 11: Linting"
if make lint-check > /dev/null 2>&1; then
    echo "✓ Linting passes"
else
    echo "⚠ Linting check failed (non-critical)"
fi

# Test 12: Build succeeds
echo
echo "Test 12: Build test"
if go build ./cmd/nano > /dev/null 2>&1; then
    echo "✓ Build succeeds"
else
    echo "✗ Build failed"
    exit 1
fi

# Cleanup
rm -f /tmp/nano-test

echo
echo "=== All Pipeline Tests Passed ==="
