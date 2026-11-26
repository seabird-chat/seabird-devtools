package main

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/seabird-chat/seabird-devtools/internal/repl"
)

// drainTimeout is how long to wait for run to finish after drain.
const drainTimeout = 2 * time.Second

// runShell runs a prompt while run prints to it, such as incoming events.
// If run fails, the prompt exits. When the prompt exits, drain (if set) asks
// run to finish cleanly before it is canceled.
func runShell(
	ctx context.Context,
	prompt, historyFile string,
	commands repl.Commands,
	run func(ctx context.Context, w io.Writer) error,
	drain func() error,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	shell, err := repl.NewShell(prompt, historyFile, commands)
	if err != nil {
		return err
	}
	defer shell.Close()

	runErr := make(chan error, 1)
	go func() {
		runErr <- run(ctx, shell.Stdout())
		cancel()
	}()

	err = shell.Run(ctx)
	if drain != nil && ctx.Err() == nil {
		err = errors.Join(err, drain())
		select {
		case e := <-runErr:
			return errors.Join(err, e)
		case <-time.After(drainTimeout):
		}
	}
	cancel()
	return errors.Join(err, <-runErr)
}
