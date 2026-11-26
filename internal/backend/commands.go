package backend

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/seabird-chat/seabird-go/pb"

	"github.com/seabird-chat/seabird-devtools/internal/format"
	"github.com/seabird-chat/seabird-devtools/internal/markdown"
	"github.com/seabird-chat/seabird-devtools/internal/repl"
)

// Commands returns a REPL command for each event the backend can emit, plus
// commands to inspect channels and control how requests are answered.
func (b *Backend) Commands() repl.Commands {
	return repl.Commands{
		b.channelMessage("msg", "send a message to a channel as a user",
			func(source *pb.ChannelSource, block *pb.Block) *pb.ChatEvent {
				return &pb.ChatEvent{Inner: &pb.ChatEvent_Message{Message: &pb.MessageEvent{Source: source, RootBlock: block}}}
			}),
		b.channelMessage("action", "perform an action (/me) in a channel as a user",
			func(source *pb.ChannelSource, block *pb.Block) *pb.ChatEvent {
				return &pb.ChatEvent{Inner: &pb.ChatEvent_Action{Action: &pb.ActionEvent{Source: source, RootBlock: block}}}
			}),
		b.channelMessage("mention", "mention the bot in a channel as a user (deprecated event)",
			func(source *pb.ChannelSource, block *pb.Block) *pb.ChatEvent {
				return &pb.ChatEvent{Inner: &pb.ChatEvent_Mention{Mention: &pb.MentionEvent{Source: source, RootBlock: block}}}
			}),
		{
			Name: "cmd",
			Args: []string{"<channel>", "<user>", "<command>", "[arg...]"},
			Help: "run a bot command in a channel as a user",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				if err := b.joined(args[0]); err != nil {
					return err
				}
				return b.send(&pb.ChatEvent{Inner: &pb.ChatEvent_Command{Command: &pb.CommandEvent{
					Source:  channelSource(args[0], args[1]),
					Command: args[2],
					Arg:     args[3],
				}}})
			},
		},
		b.privateMessage("pm", "send a private message to the bot as a user",
			func(user *pb.User, block *pb.Block) *pb.ChatEvent {
				return &pb.ChatEvent{Inner: &pb.ChatEvent_PrivateMessage{PrivateMessage: &pb.PrivateMessageEvent{Source: user, RootBlock: block}}}
			}),
		b.privateMessage("paction", "perform an action (/me) in a private message as a user",
			func(user *pb.User, block *pb.Block) *pb.ChatEvent {
				return &pb.ChatEvent{Inner: &pb.ChatEvent_PrivateAction{PrivateAction: &pb.PrivateActionEvent{Source: user, RootBlock: block}}}
			}),
		{
			Name: "join",
			Args: []string{"<channel>"},
			Help: "join a channel",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return b.Join(args[0])
			},
		},
		{
			Name: "leave",
			Args: []string{"<channel>"},
			Help: "leave a channel",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				event, err := b.leave("", args[0])
				if err != nil {
					return err
				}
				return b.send(event)
			},
		},
		{
			Name: "topic",
			Args: []string{"<channel>", "[topic...]"},
			Help: "change a channel's topic",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				event, err := b.change("", args[0], func(c *pb.Channel) { c.Topic = args[1] })
				if err != nil {
					return err
				}
				return b.send(event)
			},
		},
		{
			Name: "rename",
			Args: []string{"<channel>", "<name...>"},
			Help: "change a channel's display name",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				event, err := b.change("", args[0], func(c *pb.Channel) { c.DisplayName = args[1] })
				if err != nil {
					return err
				}
				return b.send(event)
			},
		},
		{
			Name: "channels",
			Help: "list joined channels",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				channels := b.Channels()
				if len(channels) == 0 {
					fmt.Fprintln(w, "none")
				}
				for _, c := range channels {
					fmt.Fprintln(w, format.Channel(c))
				}
				return nil
			},
		},
		{
			Name: "respond",
			Args: []string{"<mode>", "[arg...]"},
			Help: "answer requests with ok, fail [reason], ignore, or delay <duration>",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return b.setModeArgs(args[0], args[1])
			},
		},
	}
}

// channelMessage returns a command which sends a message-like event to a
// channel.
func (b *Backend) channelMessage(name, help string, event func(*pb.ChannelSource, *pb.Block) *pb.ChatEvent) repl.Command {
	return repl.Command{
		Name: name,
		Args: []string{"<channel>", "<user>", "<text...>"},
		Help: help,
		Run: func(ctx context.Context, w io.Writer, args []string) error {
			if err := b.joined(args[0]); err != nil {
				return err
			}
			block, err := markdown.Input(args[2], b.Plain)
			if err != nil {
				return err
			}
			return b.send(event(channelSource(args[0], args[1]), block))
		},
	}
}

// privateMessage returns a command which sends a message-like event from a
// user directly to the bot.
func (b *Backend) privateMessage(name, help string, event func(*pb.User, *pb.Block) *pb.ChatEvent) repl.Command {
	return repl.Command{
		Name: name,
		Args: []string{"<user>", "<text...>"},
		Help: help,
		Run: func(ctx context.Context, w io.Writer, args []string) error {
			block, err := markdown.Input(args[1], b.Plain)
			if err != nil {
				return err
			}
			return b.send(event(user(args[0]), block))
		},
	}
}

// setModeArgs parses the arguments to the respond command.
func (b *Backend) setModeArgs(mode, arg string) error {
	switch mode {
	case "ok":
		b.SetMode(ModeOK, "", 0)
	case "fail":
		if arg == "" {
			arg = "failed by seabird-dev"
		}
		b.SetMode(ModeFail, arg, 0)
	case "ignore":
		b.SetMode(ModeIgnore, "", 0)
	case "delay":
		delay, err := time.ParseDuration(strings.TrimSpace(arg))
		if err != nil {
			return fmt.Errorf("invalid delay: %w", err)
		}
		b.SetMode(ModeDelay, "", delay)
	default:
		return fmt.Errorf("unknown mode %q, expected ok, fail, ignore, or delay", mode)
	}
	return nil
}

func channelSource(channelID, name string) *pb.ChannelSource {
	return &pb.ChannelSource{ChannelId: channelID, User: user(name)}
}

func user(name string) *pb.User {
	return &pb.User{Id: name, DisplayName: name}
}
