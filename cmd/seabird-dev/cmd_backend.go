package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/peterbourgon/ff/v4"
	seabird "github.com/seabird-chat/seabird-go"

	"github.com/seabird-chat/seabird-devtools/internal/backend"
)

func newBackendCommand(rootFlags *ff.FlagSet, rootConfig *rootConfig) *ff.Command {
	var (
		b    backend.Backend
		join []string
	)

	fs := ff.NewFlagSet("backend").SetParent(rootFlags)
	fs.StringVar(&b.Type, 0, "type", "dev", "backend type reported to core")
	fs.StringVar(&b.ID, 0, "id", "local", "backend ID reported to core")
	fs.StringListVar(&join, 0, "join", "join a channel at startup (repeatable)")
	fs.BoolVar(&b.Plain, 0, "plain", "send message text as-is rather than parsing it as markdown")

	return &ff.Command{
		Name:      "backend",
		Usage:     "seabird-dev backend [FLAGS]",
		ShortHelp: "act as a fake chat backend",
		LongHelp: strings.TrimSpace(`
Start a prompt which sends chat events to core as fake users, and prints and
answers core's requests. Run "help" at the prompt to list commands.`),
		Flags: fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
			}

			client, err := seabird.NewChatIngestClient(rootConfig.Host, rootConfig.Token)
			if err != nil {
				return fmt.Errorf("connect to %s: %w", rootConfig.Host, err)
			}
			defer client.Close()

			b.JSON = rootConfig.JSON
			b.Color = useColor()

			if err := b.Connect(ctx, client.Inner); err != nil {
				return err
			}
			defer b.Close()
			for _, name := range join {
				if err := b.Join(name); err != nil {
					return err
				}
			}

			return runShell(ctx, "backend> ", historyFile("backend"), b.Commands(), b.Run, b.CloseSend)
		},
	}
}
