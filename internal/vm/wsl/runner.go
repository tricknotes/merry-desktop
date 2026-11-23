package wsl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// Result is the outcome of one wsl.exe invocation.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Process is a long-running wsl.exe invocation.
type Process interface {
	// Stop ends the process. It is safe to call more than once.
	Stop() error
	// Done is closed when the process has exited.
	Done() <-chan struct{}
}

// Runner runs wsl.exe. Tests replace it with a fake.
type Runner interface {
	Run(ctx context.Context, args ...string) (Result, error)
	Start(args ...string) (Process, error)
}

// ExecRunner runs the real wsl.exe.
type ExecRunner struct {
	// Path is the wsl.exe to run. Empty means "wsl.exe" from PATH.
	Path string
}

func (r ExecRunner) path() string {
	if r.Path != "" {
		return r.Path
	}
	return "wsl.exe"
}

func (r ExecRunner) command(ctx context.Context, args []string) *exec.Cmd {
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, r.path(), args...)
	} else {
		cmd = exec.Command(r.path(), args...)
	}
	// Without WSL_UTF8, wsl.exe prints its own messages as UTF-16.
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	hideWindow(cmd)
	return cmd
}

// Run runs wsl.exe and waits for it. A non-zero exit code is reported in the
// Result, not as an error; the error is for failures to run wsl.exe at all.
func (r ExecRunner) Run(ctx context.Context, args ...string) (Result, error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command(ctx, args)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: cleanOutput(stdout.String()), Stderr: cleanOutput(stderr.String())}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &exitErr) && ctx.Err() == nil:
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	default:
		return res, err
	}
}

// Start starts wsl.exe without waiting for it.
func (r ExecRunner) Start(args ...string) (Process, error) {
	cmd := r.command(nil, args)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &execProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

type execProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (p *execProcess) Stop() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-p.done
	return nil
}

func (p *execProcess) Done() <-chan struct{} { return p.done }

// cleanOutput drops NULs (UTF-16 output when WSL_UTF8 is ignored) and CRs.
func cleanOutput(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.ReplaceAll(s, "\r", "")
}
