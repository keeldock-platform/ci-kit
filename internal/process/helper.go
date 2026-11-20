package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

// RunHelper owns a trusted, bounded credential helper. Its caller must supply
// bounded private-memory stdout and discarded stderr; project execution uses Run.
func RunHelper(ctx context.Context, cmd *exec.Cmd) error {
	if cmd == nil || cmd.Path == "" || cmd.Stdout == nil {
		return ErrInvalid
	}
	nul, e := os.Open(os.DevNull)
	if e != nil {
		return ErrCapture
	}
	defer nul.Close()
	cmd.Stdin = nul
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 10 * time.Second
	owner, e := prepare(cmd)
	if e != nil {
		return ErrCapture
	}
	defer owner.close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if e = cmd.Start(); e != nil {
		return ErrCapture
	}
	if e = owner.started(cmd); e != nil {
		stopUnadmitted(cmd)
		return ErrCapture
	}
	exited := observeExit(cmd)
	var result error
	ended := false
	select {
	case result = <-exited:
		ended = true
	case <-ctx.Done():
		result = ctx.Err()
	}
	_ = owner.stop(false)
	if !ended {
		select {
		case e = <-exited:
			ended = true
			if result == nil {
				result = e
			}
		case <-time.After(10 * time.Second):
		}
	}
	stopErr := owner.stop(true)
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	ended = false
	if !ended {
		select {
		case e = <-waited:
			ended = true
			if result == nil {
				result = e
			}
		case <-time.After(10 * time.Second):
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		empty, probeErr := owner.empty()
		if probeErr != nil || !ended {
			return ErrCleanup
		}
		if empty {
			if !terminationConfirmed(stopErr, true) {
				return ErrCleanup
			}
			break
		}
		if time.Now().After(deadline) {
			return ErrCleanup
		}
		select {
		case <-time.After(10 * time.Millisecond):
		}
	}
	if errors.Is(result, context.DeadlineExceeded) || errors.Is(result, context.Canceled) {
		return result
	}
	if result != nil {
		return ErrCapture
	}
	return nil
}

// MakePrivateDir creates a new non-inherited user-only directory on supported OSes.
func MakePrivateDir(path string) error { return makePrivateDir(path) }

// CreatePrivateFile creates a new user-only file without inherited Windows ACEs.
func CreatePrivateFile(path string) (*os.File, error) { return privateFile(path) }

func stopUnadmitted(cmd *exec.Cmd) bool {
	killErr := cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		return killErr == nil
	case <-time.After(10 * time.Second):
		return false
	}
}
