package plugin

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	seabird "github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// fakeCore records the last request it received and returns canned responses.
type fakeCore struct {
	pb.UnimplementedSeabirdServer

	last   proto.Message
	events []*pb.Event
}

func (f *fakeCore) SendMessage(ctx context.Context, req *pb.SendMessageRequest) (*pb.SendMessageResponse, error) {
	f.last = req
	return &pb.SendMessageResponse{}, nil
}

func (f *fakeCore) LeaveChannel(ctx context.Context, req *pb.LeaveChannelRequest) (*pb.LeaveChannelResponse, error) {
	f.last = req
	return &pb.LeaveChannelResponse{}, nil
}

func (f *fakeCore) ListChannels(ctx context.Context, req *pb.ListChannelsRequest) (*pb.ListChannelsResponse, error) {
	f.last = req
	return &pb.ListChannelsResponse{Channels: []*pb.Channel{
		{Id: "sbc_a", DisplayName: "#a", Topic: "hello"},
		{Id: "sbc_b", DisplayName: "#b"},
	}}, nil
}

func (f *fakeCore) RegisteredCommands(ctx context.Context, req *pb.CommandsRequest) (*pb.CommandsResponse, error) {
	return &pb.CommandsResponse{Commands: map[string]*pb.CommandMetadata{
		"weather": {Name: "weather", ShortHelp: "<zip>", FullHelp: "show the weather"},
		"ping":    {Name: "ping"},
	}}, nil
}

func (f *fakeCore) StreamEvents(req *pb.StreamEventsRequest, stream pb.Seabird_StreamEventsServer) error {
	for _, e := range f.events {
		if err := stream.Send(e); err != nil {
			return err
		}
	}
	return nil
}

func newTestPlugin(t *testing.T, core *fakeCore) *Plugin {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterSeabirdServer(srv, core)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	assert.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return &Plugin{Client: pb.NewSeabirdClient(conn)}
}

func TestCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		plain    bool
		line     string
		wantReq  proto.Message
		wantText string
	}{
		{
			name:     "send",
			line:     "send sbc_a hello  world",
			wantReq:  &pb.SendMessageRequest{ChannelId: "sbc_a", RootBlock: seabird.NewTextBlock("hello  world")},
			wantText: "ok\n",
		},
		{
			name: "send markdown",
			line: `send sbc_a **hi**\n- one`,
			wantReq: &pb.SendMessageRequest{ChannelId: "sbc_a", RootBlock: seabird.NewContainerBlock(
				seabird.NewBoldBlock(seabird.NewTextBlock("hi")),
				seabird.NewTextBlock("\n"),
				seabird.NewListBlock(seabird.NewTextBlock("one")),
			)},
			wantText: "ok\n",
		},
		{
			name:     "send plain",
			plain:    true,
			line:     `send sbc_a **hi**\nthere`,
			wantReq:  &pb.SendMessageRequest{ChannelId: "sbc_a", RootBlock: seabird.NewTextBlock("**hi**\nthere")},
			wantText: "ok\n",
		},
		{
			name:     "leave without message",
			line:     "leave sbc_a",
			wantReq:  &pb.LeaveChannelRequest{ChannelId: "sbc_a"},
			wantText: "ok\n",
		},
		{
			name:     "channels",
			line:     "channels irc.libera",
			wantReq:  &pb.ListChannelsRequest{BackendId: "irc.libera"},
			wantText: "sbc_a name=\"#a\" topic=\"hello\"\nsbc_b name=\"#b\" topic=\"\"\n",
		},
		{
			name:     "commands sorted",
			line:     "commands",
			wantText: "ping\nweather <zip>: show the weather\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			core := &fakeCore{}
			p := newTestPlugin(t, core)
			p.Plain = tt.plain

			var buf bytes.Buffer
			assert.NoError(t, p.Commands().Dispatch(t.Context(), &buf, tt.line))
			assert.Equal(t, tt.wantText, buf.String())
			if tt.wantReq != nil {
				assert.True(t, proto.Equal(tt.wantReq, core.last), "got request %v", core.last)
			}
		})
	}
}

func TestWatch(t *testing.T) {
	t.Parallel()

	core := &fakeCore{events: []*pb.Event{{
		Inner: &pb.Event_Message{Message: &pb.MessageEvent{
			Source: &pb.ChannelSource{ChannelId: "sbc_a", User: &pb.User{Id: "sbu_x", DisplayName: "x"}},
			Text:   "hi",
		}},
	}}}
	p := newTestPlugin(t, core)

	// The fake stream ends after its events, which Watch reports as an error.
	var buf bytes.Buffer
	err := p.Watch(t.Context(), &buf, nil, 0)
	assert.Error(t, err)
	assert.Equal(t, "message sbc_a x(sbu_x): hi\n", buf.String())
}

func TestWatchLimit(t *testing.T) {
	t.Parallel()

	message := &pb.Event{Inner: &pb.Event_Message{Message: &pb.MessageEvent{
		Source: &pb.ChannelSource{ChannelId: "sbc_a", User: &pb.User{Id: "sbu_x", DisplayName: "x"}},
		Text:   "hi",
	}}}
	core := &fakeCore{events: []*pb.Event{message, message, message}}
	p := newTestPlugin(t, core)

	var buf bytes.Buffer
	assert.NoError(t, p.Watch(t.Context(), &buf, nil, 2))
	assert.Equal(t, 2, strings.Count(buf.String(), "\n"))
}
