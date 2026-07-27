package exec

import "testing"

// TestExitErrorProcessExitCode locks the --raw contract: passthrough mirrors
// the remote exit code verbatim, everything else normalizes to tri-state 1.
func TestExitErrorProcessExitCode(t *testing.T) {
	if got := (&ExitError{Code: 42}).ProcessExitCode(); got != 1 {
		t.Fatalf("normalized code = %d, want 1", got)
	}
	if got := (&ExitError{Code: 42, Passthrough: true}).ProcessExitCode(); got != 42 {
		t.Fatalf("passthrough code = %d, want 42", got)
	}
}
