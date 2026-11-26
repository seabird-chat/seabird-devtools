package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

type rootConfig struct {
	Host  string
	Token string
	JSON  bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Flags can also be set from the environment, e.g. SEABIRD_TOKEN.
	rootCmd := newRootCommand()
	err := rootCmd.ParseAndRun(ctx, os.Args[1:], ff.WithEnvVarPrefix("SEABIRD"))
	if err == nil {
		return
	}

	target := rootCmd.GetSelected()
	if target == nil {
		target = rootCmd
	}

	switch {
	case errors.Is(err, ff.ErrHelp):
		fmt.Fprintf(os.Stdout, "%s\n", ffhelp.Command(target))
		return
	case errors.Is(err, ff.ErrNoExec):
		fmt.Fprintf(os.Stderr, "%s\n", ffhelp.Command(target))
	default:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}

	os.Exit(1)
}

func newRootCommand() *ff.Command {
	rootConfig := &rootConfig{}

	fs := ff.NewFlagSet("seabird-dev")

	fs.StringVar(&rootConfig.Host, 0, "host", "http://127.0.0.1:11235", "seabird-core URL")
	fs.StringVar(&rootConfig.Token, 0, "token", "", "seabird-core auth token")
	fs.BoolVar(&rootConfig.JSON, 0, "json", "print responses and events as JSON")

	return &ff.Command{
		Name:  "seabird-dev",
		Usage: "seabird-dev [FLAGS] <SUBCOMMAND> ...",
		Flags: fs,
		Subcommands: []*ff.Command{
			newBackendCommand(fs, rootConfig),
			newPluginCommand(fs, rootConfig),
			newVersionCommand(),
		},
	}
}

// historyFile returns where to save prompt history for name, or an empty
// string to disable history if there is no usable cache directory.
func historyFile(name string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}

	dir = filepath.Join(dir, "seabird-dev")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}

	return filepath.Join(dir, name+"_history")
}
