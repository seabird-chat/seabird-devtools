package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v4"
	seabird "github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"

	"github.com/seabird-chat/seabird-devtools/internal/plugin"
	"github.com/seabird-chat/seabird-devtools/internal/repl"
)

func newPluginCommand(rootFlags *ff.FlagSet, rootConfig *rootConfig) *ff.Command {
	var commands []string

	fs := ff.NewFlagSet("plugin").SetParent(rootFlags)
	fs.StringListVar(&commands, 0, "command", "register a command as NAME or NAME:SHORT_HELP (repeatable)")

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

			p := &plugin.Plugin{Client: client.Inner, JSON: rootConfig.JSON}

			switch {
			case len(args) == 0:
				return runPluginShell(ctx, p, metadata)
			case len(args) == 1 && args[0] == "watch":
				return p.Watch(ctx, os.Stdout, metadata)
			default:
				return p.Commands().Dispatch(ctx, os.Stdout, strings.Join(args, " "))
			}
		},
	}
}

// runPluginShell runs the prompt while printing events above it. Whichever
// finishes first stops the other.
func runPluginShell(ctx context.Context, p *plugin.Plugin, metadata map[string]*pb.CommandMetadata) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	shell, err := repl.NewShell("plugin> ", historyFile("plugin"), p.Commands())
	if err != nil {
		return err
	}
	defer shell.Close()

	watchErr := make(chan error, 1)
	go func() {
		watchErr <- p.Watch(ctx, shell.Stdout(), metadata)
		cancel()
	}()

	err = shell.Run(ctx)
	cancel()
	return errors.Join(err, <-watchErr)
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
