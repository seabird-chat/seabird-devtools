// Package repl runs a table of line-oriented commands, either one at a time
// or from an interactive prompt.
package repl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
)

// Command is a single named command.
type Command struct {
	Name string

	// Args names the positional arguments. "<name>" is required and "[name]"
	// is optional. A trailing "..." on the last one makes it take the rest of
	// the line, spaces included.
	Args []string

	Help string

	// Run is called with exactly len(Args) arguments, where missing optional
	// ones are empty.
	Run func(ctx context.Context, w io.Writer, args []string) error
}

// Usage returns the command name followed by its argument names.
func (c Command) Usage() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Commands is a set of commands which can be dispatched by name.
type Commands []Command

// Dispatch parses line and runs the command it names. An empty line is
// ignored. "help" prints the command list and "sleep <duration>" waits.
func (c Commands) Dispatch(ctx context.Context, w io.Writer, line string) error {
	name, rest := cutField(line)
	if name == "" {
		return nil
	}

	switch name {
	case "help":
		return c.Help(w)
	case "sleep":
		return sleep(ctx, rest)
	}

	cmd, ok := c.find(name)
	if !ok {
		return fmt.Errorf("unknown command %q, try help", name)
	}

	args, err := parseArgs(cmd.Args, rest)
	if err != nil {
		return fmt.Errorf("%w (usage: %s)", err, cmd.Usage())
	}

	return cmd.Run(ctx, w, args)
}

// Help writes the usage and help text of every command to w.
func (c Commands) Help(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, cmd := range c {
		fmt.Fprintf(tw, "%s\t%s\n", cmd.Usage(), cmd.Help)
	}
	fmt.Fprintf(tw, "sleep <duration>\twait, such as for replies when piping commands in\n")
	fmt.Fprintf(tw, "help\tlist commands\n")
	return tw.Flush()
}

// Names returns the name of every command, including the built-in ones.
func (c Commands) Names() []string {
	names := make([]string, 0, len(c)+1)
	for _, cmd := range c {
		names = append(names, cmd.Name)
	}
	return append(names, "sleep", "help")
}

func (c Commands) find(name string) (Command, bool) {
	for _, cmd := range c {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return Command{}, false
}

// parseArgs splits line into one argument per entry in spec.
func parseArgs(spec []string, line string) ([]string, error) {
	args := make([]string, 0, len(spec))
	for i, name := range spec {
		optional := strings.HasPrefix(name, "[")
		rest := i == len(spec)-1 && strings.HasSuffix(strings.TrimRight(name, ">]"), "...")

		var arg string
		if rest {
			arg, line = strings.TrimSpace(line), ""
		} else {
			arg, line = cutField(line)
		}

		if arg == "" && !optional {
			return nil, fmt.Errorf("missing %s", name)
		}

		args = append(args, arg)
	}

	if strings.TrimSpace(line) != "" {
		return nil, errors.New("too many arguments")
	}

	return args, nil
}

// cutField returns the first whitespace-separated field of s and everything
// after it.
func cutField(s string) (field, rest string) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}

// sleep waits for the duration in arg, or until ctx is done.
func sleep(ctx context.Context, arg string) error {
	d, err := time.ParseDuration(strings.TrimSpace(arg))
	if err != nil {
		return fmt.Errorf("sleep: %w (usage: sleep <duration>)", err)
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
