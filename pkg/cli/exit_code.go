package cli

// ExitCoder marks an error with a process exit code.
type ExitCoder interface {
	error
	ExitCode() int
}

type exitCodeError struct {
	err  error
	code int
}

func (e exitCodeError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e exitCodeError) Unwrap() error {
	return e.err
}

// ExitCode returns the process exit code associated with err, defaulting to 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(ExitCoder); ok {
		return exitErr.ExitCode()
	}
	return 1
}

func (e exitCodeError) ExitCode() int {
	if e.code == 0 {
		return 1
	}
	return e.code
}

func withExitCode(err error, code int) error {
	if err == nil {
		return nil
	}
	return exitCodeError{err: err, code: code}
}
