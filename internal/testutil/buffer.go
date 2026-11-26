// Package testutil has helpers shared by tests.
package testutil

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitTimeout bounds WaitFor, so a missing write fails the test rather than
// hanging it.
const waitTimeout = 5 * time.Second

// Buffer is a bytes.Buffer which is safe to write from several goroutines, and
// which tests can wait on. The zero value is ready to use.
type Buffer struct {
	mu  sync.Mutex
	buf bytes.Buffer

	// changed is closed and cleared on each write, waking waiters.
	changed chan struct{}
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.changed != nil {
		close(b.changed)
		b.changed = nil
	}
	return b.buf.Write(p)
}

func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// WaitFor waits until the buffer contains s, failing the test if that takes
// longer than a few seconds.
func (b *Buffer) WaitFor(t testing.TB, s string) {
	t.Helper()

	timeout := time.NewTimer(waitTimeout)
	defer timeout.Stop()

	for {
		b.mu.Lock()
		if strings.Contains(b.buf.String(), s) {
			b.mu.Unlock()
			return
		}
		if b.changed == nil {
			b.changed = make(chan struct{})
		}
		changed := b.changed
		b.mu.Unlock()

		select {
		case <-changed:
		case <-timeout.C:
			t.Fatalf("timed out waiting for %q in output:\n%s", s, b.String())
		}
	}
}
