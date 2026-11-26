package repl

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
)

// lockedBuffer is a bytes.Buffer which is safe to write from several
// goroutines.
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

var echoCommands = Commands{{
	Name: "echo",
	Args: []string{"<text...>"},
	Run: func(ctx context.Context, w io.Writer, args []string) error {
		_, err := io.WriteString(w, args[0]+"\n")
		return err
	},
}}

// Shell tests don't use t.Parallel: readline sets a package-level SIGWINCH
// handler when creating an instance, so creating two at once is a data race.

// runShell runs a shell reading from stdin in the background, returning its
// output and a channel which receives the result of Run.
func runShell(t *testing.T, ctx context.Context, stdin io.Reader) (*lockedBuffer, <-chan error) {
	t.Helper()

	var out lockedBuffer
	shell, err := NewShell(ShellConfig{Prompt: "> ", Stdin: stdin, Stdout: &out}, echoCommands)
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
	// The pipe is never written to, so Run is blocked reading a line when the
	// context is canceled.
	stdin, stdinW := io.Pipe()
	t.Cleanup(func() { stdinW.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	_, done := runShell(t, ctx, stdin)

	time.Sleep(50 * time.Millisecond)
	cancel()
	assert.NoError(t, wait(t, done))
}

func TestShellStopsDuringSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	out, done := runShell(t, ctx, strings.NewReader("sleep 1h\necho after\n"))

	time.Sleep(50 * time.Millisecond)
	cancel()
	assert.NoError(t, wait(t, done))
	assert.NotContains(t, out.String(), "after")
}
