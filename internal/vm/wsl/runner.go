package wsl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sort"
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

// RunOptions are extra inputs for one wsl.exe invocation.
type RunOptions struct {
	// Stdin is written to wsl.exe's standard input.
	Stdin []byte
	// Env adds variables for the Linux side. They are passed through WSLENV,
	// so values (a proxy URL with a password, for example) never appear on the
	// command line.
	Env map[string]string
}

// StdioProcess is a long-running wsl.exe whose standard input and output form
// a connection (stdin is written, stdout is read).
type StdioProcess interface {
	Process
	io.ReadWriteCloser
}

// Runner runs wsl.exe. Tests replace it with a fake.
type Runner interface {
	Run(ctx context.Context, args ...string) (Result, error)
	RunWith(ctx context.Context, opts RunOptions, args ...string) (Result, error)
	Start(args ...string) (Process, error)
	StartStdio(args ...string) (StdioProcess, error)
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
	return r.RunWith(ctx, RunOptions{}, args...)
}

// RunWith is Run with standard input and extra environment variables.
func (r ExecRunner) RunWith(ctx context.Context, opts RunOptions, args ...string) (Result, error) {
	var stdout, stderr bytes.Buffer
	cmd := r.command(ctx, args)
	if opts.Stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}
	if len(opts.Env) > 0 {
		names := make([]string, 0, len(opts.Env))
		for k, v := range opts.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
			names = append(names, k)
		}
		sort.Strings(names)
		if existing := os.Getenv("WSLENV"); existing != "" {
			names = append([]string{existing}, names...)
		}
		cmd.Env = append(cmd.Env, "WSLENV="+strings.Join(names, ":"))
	}
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

// StartStdio starts wsl.exe with its standard input and output connected to
// the returned process. Its stderr (logs of the Linux side) is discarded.
func (r ExecRunner) StartStdio(args ...string) (StdioProcess, error) {
	cmd := r.command(nil, args)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &stdioProcess{execProcess: execProcess{cmd: cmd, done: make(chan struct{})}, in: stdin, out: stdout}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

type stdioProcess struct {
	execProcess
	in  io.WriteCloser
	out io.ReadCloser
}

func (p *stdioProcess) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *stdioProcess) Write(b []byte) (int, error) { return p.in.Write(b) }
func (p *stdioProcess) Close() error {
	_ = p.in.Close()
	return p.Stop()
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
