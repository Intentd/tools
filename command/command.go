// Package command is the one way to run a process: a Command says what to
// run and how, Run runs it, and the Result holds what came back. A non-zero
// exit is data (Result.ExitCode), not an error; Run's error means the
// command could not run at all: not found, timed out, or cancelled. Tests
// replace every Run at once with Fake.
package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Command is a process to run: Shell through `sh -c`, or Args (a program and
// its arguments, no shell).
type Command struct {
	// Shell is a script run with `sh -c`.
	Shell string
	// Args is a program and its arguments, run without a shell.
	Args []string
	// Dir is the working directory; empty is the current one.
	Dir string
	// Env is added to the process environment, its values winning.
	Env map[string]string
	// Stdin is the process's standard input; empty is none.
	Stdin string
	// Timeout bounds the run; zero is no bound beyond the context's.
	Timeout time.Duration
	// Stream receives the output live as well (a build's log), stdout and
	// stderr together as they arrive.
	Stream io.Writer
}

// Args is a Command running program with args, no shell.
func Args(program string, args ...string) Command {
	return Command{Args: append([]string{program}, args...)}
}

// Shell is a Command running script with `sh -c`.
func Shell(script string) Command {
	return Command{Shell: script}
}

// String is the command as written, for messages.
func (c Command) String() string {
	if c.Shell != "" {
		return c.Shell
	}
	return strings.Join(c.Args, " ")
}

// Result is what a command gave back.
type Result struct {
	// Command is what ran, for messages.
	Command string
	Stdout  string
	Stderr  string
	// Output is stdout and stderr together as they arrived: each stream in
	// its own order, the two interleaved as read.
	Output   string
	ExitCode int
	Duration time.Duration
	// streamed is set when the output went to the command's Stream, so an
	// error doesn't repeat it.
	streamed bool
}

// tailLines is how much of the output Err keeps.
const tailLines = 20

// Err is an *ExitError when the command exited non-zero, nil otherwise. Its
// Output is empty when the output was streamed: it has been shown already.
func (r Result) Err() error {
	if r.ExitCode == 0 {
		return nil
	}
	e := &ExitError{Command: r.Command, Code: r.ExitCode}
	if !r.streamed {
		e.Output = r.Output
	}
	return e
}

// ExitError is a command that ran and exited non-zero.
type ExitError struct {
	Command string
	Code    int
	Output  string
}

// Error names the command, its exit code and the tail of its output.
func (e *ExitError) Error() string {
	msg := fmt.Sprintf("%s: exit code %d", e.Command, e.Code)
	if tail := Tail(e.Output, tailLines); tail != "" {
		msg += "\n" + tail
	}
	return msg
}

// Tail is the last n lines of out, trimmed.
func Tail(out string, n int) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

var (
	fakeMu sync.RWMutex
	fake   func(Command) Result
)

// Fake makes every Run call fn instead of starting a process, until restore
// is called. fn's Result is returned as Run would; its Output is written to
// the command's Stream. Tests that fake do not run in parallel.
func Fake(fn func(Command) Result) (restore func()) {
	fakeMu.Lock()
	prev := fake
	fake = fn
	fakeMu.Unlock()
	return func() {
		fakeMu.Lock()
		fake = prev
		fakeMu.Unlock()
	}
}

// Run runs c and waits for it. The error is non-nil only when it could not
// run: an empty command, a program not found, the timeout or the context
// ending. A non-zero exit is Result.ExitCode; Result.Err turns it into an
// error.
func Run(ctx context.Context, c Command) (Result, error) {
	fakeMu.RLock()
	fn := fake
	fakeMu.RUnlock()
	if fn != nil {
		r := fn(c)
		r.Command = c.String()
		if c.Stream != nil {
			r.streamed = true
			_, _ = io.WriteString(c.Stream, r.Output)
		}
		return r, nil
	}
	return run(ctx, c)
}

// Exec runs c for its effect: the error is non-nil when it could not run or
// exited non-zero. Set Stream to show the output as it runs.
func Exec(ctx context.Context, c Command) error {
	r, err := Run(ctx, c)
	if err != nil {
		return err
	}
	return r.Err()
}

// Output runs c and returns its stdout, trimmed; a command that could not
// run or exited non-zero is an error.
func Output(ctx context.Context, c Command) (string, error) {
	r, err := Run(ctx, c)
	if err != nil {
		return "", err
	}
	if err := r.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(r.Stdout), nil
}

// Succeeds runs c and reports whether it ran and exited zero.
func Succeeds(ctx context.Context, c Command) bool {
	r, err := Run(ctx, c)
	return err == nil && r.ExitCode == 0
}

func run(ctx context.Context, c Command) (Result, error) {
	res := Result{Command: c.String(), streamed: c.Stream != nil}
	var name string
	var args []string
	switch {
	case c.Shell != "":
		name, args = "sh", []string{"-c", c.Shell}
	case len(c.Args) > 0:
		name, args = c.Args[0], c.Args[1:]
	default:
		return res, errors.New("command: nothing to run")
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), pairs(c.Env)...)
	}
	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}

	var stdout, stderr bytes.Buffer
	combined := &lockedBuffer{}
	both := io.Writer(combined)
	if c.Stream != nil {
		both = &lockedWriter{w: io.MultiWriter(combined, c.Stream)}
	}
	cmd.Stdout = io.MultiWriter(&stdout, both)
	cmd.Stderr = io.MultiWriter(&stderr, both)

	start := time.Now()
	err := cmd.Run()
	res.Duration = time.Since(start)
	res.Stdout, res.Stderr, res.Output = stdout.String(), stderr.String(), combined.String()

	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) && c.Timeout > 0 {
			return res, fmt.Errorf("%s: timed out after %s", res.Command, c.Timeout)
		}
		return res, fmt.Errorf("%s: %w", res.Command, ctxErr)
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.ExitCode = exit.ExitCode()
	default:
		return res, fmt.Errorf("%s: %w", res.Command, err)
	}
	return res, nil
}

// pairs is env as KEY=VALUE entries, in key order.
func pairs(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// lockedWriter serializes the two streams' writes to one writer.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// lockedBuffer is a buffer two writers (stdout, stderr) share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
