// Package plugin exposes every seabird-core plugin RPC as a REPL command.
package plugin

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/seabird-chat/seabird-go/pb"
	"google.golang.org/protobuf/proto"

	"github.com/seabird-chat/seabird-devtools/internal/format"
	"github.com/seabird-chat/seabird-devtools/internal/markdown"
	"github.com/seabird-chat/seabird-devtools/internal/repl"
)

const rpcTimeout = 5 * time.Second

// Plugin calls seabird-core on behalf of REPL commands.
type Plugin struct {
	Client pb.SeabirdClient

	// JSON prints responses and events as protojson rather than text.
	JSON bool

	// Plain sends message text as-is rather than parsing it as markdown.
	Plain bool

	// Color renders formatting in events with terminal escape codes.
	Color bool
}

// Watch streams events to w until ctx is done or the stream fails. Commands
// are registered with core so that matching command events are delivered.
func (p *Plugin) Watch(ctx context.Context, w io.Writer, commands map[string]*pb.CommandMetadata) error {
	stream, err := p.Client.StreamEvents(ctx, &pb.StreamEventsRequest{Commands: commands})
	if err != nil {
		return fmt.Errorf("stream events: %w", err)
	}

	for {
		event, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("stream events: %w", err)
		}

		p.print(w, event, format.Event(event, p.Color))
	}
}

// Commands returns a REPL command for each RPC.
func (p *Plugin) Commands() repl.Commands {
	return repl.Commands{
		{
			Name: "send",
			Args: []string{"<channel>", "<text...>"},
			Help: "send a message to a channel",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				block, err := markdown.Input(args[1], p.Plain)
				if err != nil {
					return err
				}
				return p.call(ctx, w, "send message", func(ctx context.Context) (proto.Message, error) {
					return p.Client.SendMessage(ctx, &pb.SendMessageRequest{
						ChannelId: args[0],
						RootBlock: block,
					})
				})
			},
		},
		{
			Name: "pm",
			Args: []string{"<user>", "<text...>"},
			Help: "send a private message to a user",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				block, err := markdown.Input(args[1], p.Plain)
				if err != nil {
					return err
				}
				return p.call(ctx, w, "send private message", func(ctx context.Context) (proto.Message, error) {
					return p.Client.SendPrivateMessage(ctx, &pb.SendPrivateMessageRequest{
						UserId:    args[0],
						RootBlock: block,
					})
				})
			},
		},
		{
			Name: "action",
			Args: []string{"<channel>", "<text...>"},
			Help: "perform an action (/me) in a channel",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				block, err := markdown.Input(args[1], p.Plain)
				if err != nil {
					return err
				}
				return p.call(ctx, w, "perform action", func(ctx context.Context) (proto.Message, error) {
					return p.Client.PerformAction(ctx, &pb.PerformActionRequest{
						ChannelId: args[0],
						RootBlock: block,
					})
				})
			},
		},
		{
			Name: "paction",
			Args: []string{"<user>", "<text...>"},
			Help: "perform an action (/me) in a private message",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				block, err := markdown.Input(args[1], p.Plain)
				if err != nil {
					return err
				}
				return p.call(ctx, w, "perform private action", func(ctx context.Context) (proto.Message, error) {
					return p.Client.PerformPrivateAction(ctx, &pb.PerformPrivateActionRequest{
						UserId:    args[0],
						RootBlock: block,
					})
				})
			},
		},
		{
			Name: "join",
			Args: []string{"<backend>", "<channel-name>"},
			Help: "join a channel by name",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "join channel", func(ctx context.Context) (proto.Message, error) {
					return p.Client.JoinChannel(ctx, &pb.JoinChannelRequest{
						BackendId:   args[0],
						ChannelName: args[1],
					})
				})
			},
		},
		{
			Name: "leave",
			Args: []string{"<channel>", "[message...]"},
			Help: "leave a channel",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "leave channel", func(ctx context.Context) (proto.Message, error) {
					return p.Client.LeaveChannel(ctx, &pb.LeaveChannelRequest{
						ChannelId:   args[0],
						ExitMessage: args[1],
					})
				})
			},
		},
		{
			Name: "topic",
			Args: []string{"<channel>", "<topic...>"},
			Help: "set a channel's topic",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "update channel info", func(ctx context.Context) (proto.Message, error) {
					return p.Client.UpdateChannelInfo(ctx, &pb.UpdateChannelInfoRequest{
						ChannelId: args[0],
						Topic:     args[1],
					})
				})
			},
		},
		{
			Name: "backends",
			Help: "list connected backends",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "list backends", func(ctx context.Context) (proto.Message, error) {
					return p.Client.ListBackends(ctx, &pb.ListBackendsRequest{})
				})
			},
		},
		{
			Name: "backend",
			Args: []string{"<backend>"},
			Help: "show a backend and its metadata",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "get backend info", func(ctx context.Context) (proto.Message, error) {
					return p.Client.GetBackendInfo(ctx, &pb.BackendInfoRequest{BackendId: args[0]})
				})
			},
		},
		{
			Name: "channels",
			Args: []string{"<backend>"},
			Help: "list a backend's channels",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "list channels", func(ctx context.Context) (proto.Message, error) {
					return p.Client.ListChannels(ctx, &pb.ListChannelsRequest{BackendId: args[0]})
				})
			},
		},
		{
			Name: "channel",
			Args: []string{"<channel>"},
			Help: "show a channel",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "get channel info", func(ctx context.Context) (proto.Message, error) {
					return p.Client.GetChannelInfo(ctx, &pb.ChannelInfoRequest{ChannelId: args[0]})
				})
			},
		},
		{
			Name: "core",
			Help: "show core's startup and current time",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "get core info", func(ctx context.Context) (proto.Message, error) {
					return p.Client.GetCoreInfo(ctx, &pb.CoreInfoRequest{})
				})
			},
		},
		{
			Name: "commands",
			Help: "list commands registered by all plugins",
			Run: func(ctx context.Context, w io.Writer, args []string) error {
				return p.call(ctx, w, "registered commands", func(ctx context.Context) (proto.Message, error) {
					return p.Client.RegisteredCommands(ctx, &pb.CommandsRequest{})
				})
			},
		},
	}
}

// call runs an RPC with a timeout and prints its response.
func (p *Plugin) call(ctx context.Context, w io.Writer, name string, rpc func(context.Context) (proto.Message, error)) error {
	ctx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := rpc(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	p.print(w, resp, response(resp))
	return nil
}

func (p *Plugin) print(w io.Writer, msg proto.Message, text string) {
	if p.JSON {
		text = format.JSON(msg)
	}
	fmt.Fprintln(w, text)
}

// response renders an RPC response. Responses with no fields print "ok".
func response(msg proto.Message) string {
	switch resp := msg.(type) {
	case *pb.ListBackendsResponse:
		return lines(resp.GetBackends(), format.Backend)
	case *pb.BackendInfoResponse:
		return format.Backend(resp.GetBackend()) + format.Tags(resp.GetMetadata())
	case *pb.ListChannelsResponse:
		return lines(resp.GetChannels(), format.Channel)
	case *pb.ChannelInfoResponse:
		return format.Channel(resp.GetChannel())
	case *pb.CoreInfoResponse:
		return fmt.Sprintf("started %s, now %s",
			unixTime(resp.GetStartupTimestamp()), unixTime(resp.GetCurrentTimestamp()))
	case *pb.CommandsResponse:
		cmds := make([]*pb.CommandMetadata, 0, len(resp.GetCommands()))
		for _, cmd := range resp.GetCommands() {
			cmds = append(cmds, cmd)
		}
		slices.SortFunc(cmds, func(a, b *pb.CommandMetadata) int { return strings.Compare(a.GetName(), b.GetName()) })
		return lines(cmds, format.Command)
	default:
		return "ok"
	}
}

// lines renders each item on its own line, or "none" when there are no items.
func lines[T any](items []T, render func(T) string) string {
	if len(items) == 0 {
		return "none"
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, render(item))
	}
	return strings.Join(out, "\n")
}

func unixTime(secs uint64) string {
	return time.Unix(int64(secs), 0).Format(time.RFC3339)
}
