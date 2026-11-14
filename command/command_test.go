package command

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	ctx := context.Background()

	t.Run("stdout, stderr and both", func(t *testing.T) {
		r, err := Run(ctx, Shell("echo out; echo err >&2; echo again"))
		require.NoError(t, err)
		assert.Equal(t, "out\nagain\n", r.Stdout)
		assert.Equal(t, "err\n", r.Stderr)
		assert.ElementsMatch(t, []string{"out", "err", "again"}, strings.Fields(r.Output))
		assert.Less(t, strings.Index(r.Output, "out"), strings.Index(r.Output, "again"))
		assert.Zero(t, r.ExitCode)
		assert.NoError(t, r.Err())
	})

	t.Run("a non-zero exit is data", func(t *testing.T) {
		r, err := Run(ctx, Shell("echo boom; exit 3"))
		require.NoError(t, err)
		assert.Equal(t, 3, r.ExitCode)
		assert.ErrorContains(t, r.Err(), "exit code 3")
		assert.ErrorContains(t, r.Err(), "boom")
		var exit *ExitError
		require.ErrorAs(t, r.Err(), &exit)
		assert.Equal(t, 3, exit.Code)
	})

	t.Run("args without a shell", func(t *testing.T) {
		r, err := Run(ctx, Args("echo", "a b", "$HOME"))
		require.NoError(t, err)
		assert.Equal(t, "a b $HOME\n", r.Stdout)
	})

	t.Run("dir, env and stdin", func(t *testing.T) {
		dir := t.TempDir()
		r, err := Run(ctx, Command{
			Shell: `pwd; echo "$GREETING"; cat`,
			Dir:   dir,
			Env:   map[string]string{"GREETING": "hello"},
			Stdin: "in",
		})
		require.NoError(t, err)
		assert.Contains(t, r.Stdout, dir)
		assert.Contains(t, r.Stdout, "hello\nin")
	})

	t.Run("stream gets the output live", func(t *testing.T) {
		var live bytes.Buffer
		_, err := Run(ctx, Command{Shell: "echo one; echo two >&2", Stream: &live})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"one", "two"}, strings.Fields(live.String()))
	})

	t.Run("not found is an error", func(t *testing.T) {
		_, err := Run(ctx, Args("no-such-program-here"))
		assert.Error(t, err)
	})

	t.Run("timeout is an error", func(t *testing.T) {
		_, err := Run(ctx, Command{Shell: "sleep 5", Timeout: 50 * time.Millisecond})
		assert.ErrorContains(t, err, "timed out")
	})

	t.Run("nothing to run", func(t *testing.T) {
		_, err := Run(ctx, Command{})
		assert.Error(t, err)
	})
}

func TestOutputAndSucceeds(t *testing.T) {
	ctx := context.Background()
	out, err := Output(ctx, Shell("echo ' value '"))
	require.NoError(t, err)
	assert.Equal(t, "value", out)

	_, err = Output(ctx, Shell("exit 1"))
	assert.Error(t, err)

	assert.True(t, Succeeds(ctx, Shell("true")))
	assert.False(t, Succeeds(ctx, Shell("false")))
}

func TestFake(t *testing.T) {
	var seen []string
	restore := Fake(func(c Command) Result {
		seen = append(seen, c.String())
		return Result{Stdout: "faked\n", Output: "faked\n", ExitCode: 2}
	})
	var live bytes.Buffer
	r, err := Run(context.Background(), Command{Args: []string{"docker", "build", "."}, Stream: &live})
	require.NoError(t, err)
	assert.Equal(t, 2, r.ExitCode)
	assert.Equal(t, "docker build .", r.Command)
	assert.Equal(t, "faked\n", live.String())
	assert.Equal(t, []string{"docker build ."}, seen)

	restore()
	r, err = Run(context.Background(), Shell("echo real"))
	require.NoError(t, err)
	assert.Equal(t, "real\n", r.Stdout)
}

func TestExec(t *testing.T) {
	ctx := context.Background()
	assert.NoError(t, Exec(ctx, Shell("true")))

	err := Exec(ctx, Shell("echo kept; exit 2"))
	assert.ErrorContains(t, err, "exit code 2")
	assert.ErrorContains(t, err, "kept")

	var live bytes.Buffer
	err = Exec(ctx, Command{Shell: "echo shown; exit 2", Stream: &live})
	assert.ErrorContains(t, err, "exit code 2")
	assert.Equal(t, "echo shown; exit 2: exit code 2", err.Error(), "streamed output is not repeated")
	assert.Equal(t, "shown\n", live.String())
}

func TestTail(t *testing.T) {
	assert.Equal(t, "c\nd", Tail("a\nb\nc\nd\n", 2))
	assert.Equal(t, "", Tail("  \n", 3))
}
