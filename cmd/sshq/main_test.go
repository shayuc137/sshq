package main

import (
	"errors"
	"fmt"
	"testing"

	sshqexec "github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/output"
)

func TestProcessExitCodeThreeStates(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "remote non-zero", err: &sshqexec.ExitError{Code: 42}, want: 1},
		{name: "bad data result", err: output.BadNews(), want: 1},
		{name: "command error", err: output.Errorf("failed", ""), want: 2},
		{name: "uncategorized error", err: errors.New("failed"), want: 2},
		{name: "wrapped command error", err: fmt.Errorf("wrapped: %w", output.Errorf("failed", "")), want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := processExitCode(tt.err); got != tt.want {
				t.Fatalf("processExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
