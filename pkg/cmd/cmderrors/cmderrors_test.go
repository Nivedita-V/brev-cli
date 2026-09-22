package cmderrors

import (
	"errors"
	"testing"
)

type codedError struct {
	code int
}

func (err codedError) Error() string {
	return "coded failure"
}

func (err codedError) ExitCode() int {
	return err.code
}

func TestExitCodeUsesStableCodedErrorAndDefaultsToOne(t *testing.T) {
	if got := ExitCode(codedError{code: 23}); got != 23 {
		t.Fatalf("ExitCode(coded error) = %d, want 23", got)
	}
	if got := ExitCode(errors.New("ordinary failure")); got != 1 {
		t.Fatalf("ExitCode(ordinary error) = %d, want 1", got)
	}
}
