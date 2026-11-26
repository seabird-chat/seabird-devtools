package repl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ergochat/readline"
	"golang.org/x/term"
)

// Shell is an interactive prompt for a set of commands. Output written to
// Stdout while the prompt is shown is drawn above it, so events can be
// printed from other goroutines.
type Shell struct {
	rl       *readline.Instance
	commands Commands
}

// ShellConfig configures a Shell.
type ShellConfig struct {
	Prompt string

	// HistoryFile is where history is saved, or empty to disable it.
	HistoryFile string

	// Stdin and Stdout default to the process's. Setting either one disables
	// line editing, as it needs a real terminal.
	Stdin  io.Reader
	Stdout io.Writer
}

// NewShell creates a prompt for commands. Line editing is used only when
// stdin and stdout are a terminal and TERM isn't "dumb". Line editing asks the
// terminal for the cursor position and waits for the answer, which a dumb
// terminal never sends.
func NewShell(cfg ShellConfig, commands Commands) (*Shell, error) {
	items := make([]*readline.PrefixCompleter, 0, len(commands))
	for _, name := range commands.Names() {
		items = append(items, readline.PcItem(name))
	}

	custom := cfg.Stdin != nil || cfg.Stdout != nil
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:       cfg.Prompt,
		HistoryFile:  cfg.HistoryFile,
		AutoComplete: readline.NewPrefixCompleter(items...),
		Stdin:        cfg.Stdin,
		Stdout:       cfg.Stdout,
		FuncIsTerminal: func() bool {
			return !custom && os.Getenv("TERM") != "dumb" &&
				term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("start prompt: %w", err)
	}

	return &Shell{rl: rl, commands: commands}, nil
}

// Stdout returns a writer which is safe to use while the prompt is shown.
func (s *Shell) Stdout() io.Writer {
	return s.rl.Stdout()
}

// Close restores the terminal.
func (s *Shell) Close() error {
	return s.rl.Close()
}

// Run reads and dispatches commands until EOF, Ctrl-C on an empty line, or
// ctx is done. Command errors are printed rather than returned.
func (s *Shell) Run(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { s.rl.Close() })
	defer stop()

	w := s.Stdout()
	for {
		line, err := s.rl.ReadLine()
		switch {
		case errors.Is(err, readline.ErrInterrupt):
			if line == "" {
				return nil
			}
			continue
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read line: %w", err)
		}

		if line == "quit" || line == "exit" {
			return nil
		}

		err = s.commands.Dispatch(ctx, w, line)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			fmt.Fprintf(w, "error: %v\n", err)
		}
	}
}
