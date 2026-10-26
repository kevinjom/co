package main

// exitError is a failure with a process exit code.
type exitError struct {
	Code int
	Err  error
}

func (e *exitError) Error() string {
	return e.Err.Error()
}

func (e *exitError) Unwrap() error {
	return e.Err
}

func usageError(err error) error {
	return &exitError{Code: 2, Err: err}
}
