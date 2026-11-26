package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v4"
	"github.com/seabird-chat/seabird-go/pb"
	"golang.org/x/term"

	"github.com/seabird-chat/seabird-devtools/internal/plugin"
	"github.com/seabird-chat/seabird-devtools/internal/repl"
)

func newPluginCommand(rootFlags *ff.FlagSet, rootConfig *rootConfig) *ff.Command {
	var (
		commands []string
		plain    bool
		count    int
	)

	fs := ff.NewFlagSet("plugin").SetParent(rootFlags)
	fs.StringListVar(&commands, 0, "command", "register a command as NAME or NAME:SHORT_HELP (repeatable)")
	fs.BoolVar(&plain, 0, "plain", "send message text as-is rather than parsing it as markdown")

	watchFlags := ff.NewFlagSet("watch").SetParent(fs)
	watchFlags.IntVar(&count, 0, "count", 0, "exit after this many events (0 for no limit)")

	// connect returns a plugin connected to core, and a function to close it.
	connect := func() (*plugin.Plugin, map[string]*pb.CommandMetadata, func(), error) {
		metadata, err := parseCommandFlags(commands)
		if err != nil {
			return nil, nil, nil, err
		}

		conn, err := dial(rootConfig.Host, rootConfig.Token)
		if err != nil {
			return nil, nil, nil, err
		}

		p := &plugin.Plugin{
			Client: pb.NewSeabirdClient(conn),
			JSON:   rootConfig.JSON,
			Plain:  plain,
			Color:  useColor(),
		}
		return p, metadata, func() { conn.Close() }, nil
	}

	return &ff.Command{
		Name:      "plugin",
		Usage:     "seabird-dev plugin [FLAGS] [<COMMAND> [ARGS...]]",
		ShortHelp: "act as a plugin which can call every core RPC",
		LongHelp: strings.TrimSpace(`
With no arguments, start a prompt which prints events as they arrive.
Otherwise, run a single command, such as "seabird-dev plugin backends". Run
"seabird-dev plugin help" to list commands.`),
		Flags: fs,
		Subcommands: []*ff.Command{{
			Name:      "watch",
			Usage:     "seabird-dev plugin watch [FLAGS]",
			ShortHelp: "print events until interrupted, or until --count have arrived",
			Flags:     watchFlags,
			Exec: func(ctx context.Context, args []string) error {
				p, metadata, closeConn, err := connect()
				if err != nil {
					return err
				}
				defer closeConn()

				return p.Watch(ctx, os.Stdout, metadata, count)
			},
		}},
		Exec: func(ctx context.Context, args []string) error {
			p, metadata, closeConn, err := connect()
			if err != nil {
				return err
			}
			defer closeConn()

			if len(args) > 0 {
				return p.Commands().Dispatch(ctx, os.Stdout, strings.Join(args, " "))
			}

			shell := repl.ShellConfig{Prompt: "plugin> ", HistoryFile: historyFile("plugin")}
			return runShell(ctx, shell, p.Commands(),
				func(ctx context.Context, w io.Writer) error { return p.Watch(ctx, w, metadata, 0) },
				nil)
		},
	}
}

// parseCommandFlags turns NAME[:SHORT_HELP] values into command metadata.
func parseCommandFlags(values []string) (map[string]*pb.CommandMetadata, error) {
	metadata := make(map[string]*pb.CommandMetadata, len(values))
	for _, v := range values {
		name, help, _ := strings.Cut(v, ":")
		if name == "" {
			return nil, fmt.Errorf("invalid --command %q: missing name", v)
		}
		metadata[name] = &pb.CommandMetadata{Name: name, ShortHelp: help}
	}
	return metadata, nil
}

// useColor reports whether stdout is a terminal and NO_COLOR is unset.
func useColor() bool {
	return os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(os.Stdout.Fd()))
}
