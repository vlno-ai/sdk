// Package harness owns one explicitly requested local child, never recovery.
package harness

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"time"
)

type Spec struct {
	Argv, Env      []string
	Cwd            string
	Input          []byte
	Stdout, Stderr io.Writer
}
type Result struct {
	Status                string
	Started, ExitObserved bool
	ExitCode              int
	Err                   error
}

// Run never repeats Start. A saved PID is never accepted as process ownership.
func Run(ctx context.Context, s Spec, started func(int) error) Result {
	result := Result{Status: "error"}
	if len(s.Argv) == 0 || len(s.Input) > 4*1024*1024 {
		return result
	}
	if ctx.Err() != nil {
		result.Status = "timeout"
		result.Err = ctx.Err()
		return result
	}
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.WaitDelay = time.Second
	cmd.Env = s.Env
	cmd.Dir = s.Cwd
	cmd.Stdin = bytes.NewReader(append(s.Input, '\n'))
	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	prepare(cmd)
	if e := cmd.Start(); e != nil {
		result.Err = e
		return result
	}
	result.Started = true
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if started != nil {
		if e := started(cmd.Process.Pid); e != nil {
			kill(cmd)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
			result.Err = e
			return result
		}
	}
	select {
	case e := <-done:
		kill(cmd)
		result.ExitObserved = true
		result.ExitCode = cmd.ProcessState.ExitCode()
		result.Err = e
		if ctx.Err() != nil {
			result.Status = "timeout"
			result.Err = ctx.Err()
		} else if e == nil {
			result.Status = "completed"
		}
		return result
	case <-ctx.Done():
		kill(cmd)
		result.Status = "timeout"
		result.Err = ctx.Err()
		select {
		case <-done:
			result.ExitObserved = true
			result.ExitCode = cmd.ProcessState.ExitCode()
		case <-time.After(2 * time.Second):
		}
		return result
	}
}
