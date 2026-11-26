package repl

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"

	"github.com/seabird-chat/seabird-devtools/internal/testutil"
)

var echoCommand = Command{
	Name: "echo",
	Args: []string{"<text...>"},
	Run: func(ctx context.Context, w io.Writer, args []string) error {
		_, err := io.WriteString(w, args[0]+"\n")
		return err
	},
}

// signalCommand returns a command which closes started when it runs. If block
// is set, it then waits for ctx to be done and returns its error, like sleep.
func signalCommand(name string, block bool) (Command, <-chan struct{}) {
	started := make(chan struct{})
	return Command{
		Name: name,
		Run: func(ctx context.Context, w io.Writer, args []string) error {
			close(started)
			if !block {
				return nil
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}, started
}

// Shell tests don't use t.Parallel: readline sets a package-level SIGWINCH
// handler when creating an instance, so creating two at once is a data race.

// runShell runs a shell reading from stdin in the background, returning its
// output and a channel which receives the result of Run.
func runShell(t *testing.T, ctx context.Context, stdin io.Reader, commands ...Command) (*testutil.Buffer, <-chan error) {
	t.Helper()

	var out testutil.Buffer
	shell, err := NewShell(ShellConfig{Prompt: "> ", Stdin: stdin, Stdout: &out}, append(Commands{echoCommand}, commands...))
	assert.NoError(t, err)
	t.Cleanup(func() { shell.Close() })

	done := make(chan error, 1)
	go func() { done <- shell.Run(ctx) }()
	return &out, done
}

// wait returns the result of Run, failing if it doesn't return promptly.
func wait(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func TestShellRunsLines(t *testing.T) {
	out, done := runShell(t, t.Context(), strings.NewReader("echo hi there\nbogus\necho after\n"))

	assert.NoError(t, wait(t, done))
	assert.Contains(t, out.String(), "hi there\n")
	assert.Contains(t, out.String(), `error: unknown command "bogus", try help`)
	assert.Contains(t, out.String(), "after\n")
}

func TestShellQuit(t *testing.T) {
	out, done := runShell(t, t.Context(), strings.NewReader("quit\necho unreachable\n"))

	assert.NoError(t, wait(t, done))
	assert.NotContains(t, out.String(), "unreachable")
}

func TestShellStopsWhenCanceled(t *testing.T) {
	// After "mark" runs, Run goes back to reading from a pipe with nothing more
	// in it, so only canceling can make it return.
	stdin, stdinW := io.Pipe()
	t.Cleanup(func() { stdinW.Close() })
	go func() { _, _ = io.WriteString(stdinW, "mark\n") }()

	mark, marked := signalCommand("mark", false)
	ctx, cancel := context.WithCancel(t.Context())
	_, done := runShell(t, ctx, stdin, mark)

	<-marked
	cancel()
	assert.NoError(t, wait(t, done))
}

func TestShellStopsDuringCommand(t *testing.T) {
	block, started := signalCommand("block", true)
	ctx, cancel := context.WithCancel(t.Context())
	out, done := runShell(t, ctx, strings.NewReader("block\necho after\n"), block)

	<-started
	cancel()
	assert.NoError(t, wait(t, done))

	// Canceling isn't a command failure, and nothing after it runs.
	assert.Equal(t, "", out.String())
}
