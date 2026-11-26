package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v4"
	seabird "github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
	"golang.org/x/term"

	"github.com/seabird-chat/seabird-devtools/internal/plugin"
)

func newPluginCommand(rootFlags *ff.FlagSet, rootConfig *rootConfig) *ff.Command {
	var (
		commands []string
		plain    bool
	)

	fs := ff.NewFlagSet("plugin").SetParent(rootFlags)
	fs.StringListVar(&commands, 0, "command", "register a command as NAME or NAME:SHORT_HELP (repeatable)")
	fs.BoolVar(&plain, 0, "plain", "send message text as-is rather than parsing it as markdown")

	return &ff.Command{
		Name:      "plugin",
		Usage:     "seabird-dev plugin [FLAGS] [watch | <COMMAND> [ARGS...]]",
		ShortHelp: "act as a plugin which can call every core RPC",
		LongHelp: strings.TrimSpace(`
With no arguments, start a prompt which prints events as they arrive. With
"watch", print events until interrupted. Otherwise, run a single command, such
as "seabird-dev plugin backends". Run "seabird-dev plugin help" to list
commands.`),
		Flags: fs,
		Exec: func(ctx context.Context, args []string) error {
			metadata, err := parseCommandFlags(commands)
			if err != nil {
				return err
			}

			client, err := seabird.NewClient(rootConfig.Host, rootConfig.Token)
			if err != nil {
				return fmt.Errorf("connect to %s: %w", rootConfig.Host, err)
			}
			defer client.Close()

			p := &plugin.Plugin{
				Client: client.Inner,
				JSON:   rootConfig.JSON,
				Plain:  plain,
				Color:  useColor(),
			}

			switch {
			case len(args) == 0:
				return runShell(ctx, "plugin> ", historyFile("plugin"), p.Commands(),
					func(ctx context.Context, w io.Writer) error { return p.Watch(ctx, w, metadata) },
					nil)
			case len(args) == 1 && args[0] == "watch":
				return p.Watch(ctx, os.Stdout, metadata)
			default:
				return p.Commands().Dispatch(ctx, os.Stdout, strings.Join(args, " "))
			}
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
