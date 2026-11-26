package backend

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	seabird "github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// fakeCore forwards events from the backend to events, and requests to the
// backend from requests.
type fakeCore struct {
	pb.UnimplementedChatIngestServer

	events   chan *pb.ChatEvent
	requests chan *pb.ChatRequest
}

func (f *fakeCore) IngestEvents(stream pb.ChatIngest_IngestEventsServer) error {
	ctx := stream.Context()
	go func() {
		for {
			select {
			case req := <-f.requests:
				if err := stream.Send(req); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		event, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		f.events <- event
	}
}

// next returns the next event the backend sent, failing if none arrives.
func (f *fakeCore) next(t *testing.T) *pb.ChatEvent {
	t.Helper()

	select {
	case event := <-f.events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

// lockedBuffer is a bytes.Buffer which is safe to write from Run.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newFakeCore() *fakeCore {
	return &fakeCore{events: make(chan *pb.ChatEvent, 10), requests: make(chan *pb.ChatRequest)}
}

// dial serves core over an in-memory listener and returns a client for it.
func dial(t *testing.T, core *fakeCore) pb.ChatIngestClient {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterChatIngestServer(srv, core)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	assert.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return pb.NewChatIngestClient(conn)
}

// newTestBackend connects a backend to a fake core and runs it until the
// test ends. The hello event is consumed.
func newTestBackend(t *testing.T) (*Backend, *fakeCore, *lockedBuffer) {
	t.Helper()

	core := newFakeCore()
	ctx, cancel := context.WithCancel(t.Context())
	b := &Backend{Type: "dev", ID: "local"}
	assert.NoError(t, b.Connect(ctx, dial(t, core)))

	hello := core.next(t)
	assert.True(t, proto.Equal(&pb.ChatEvent{Inner: &pb.ChatEvent_Hello{Hello: &pb.HelloChatEvent{
		BackendInfo: &pb.Backend{Type: "dev", Id: "local"},
	}}}, hello), "got %v", hello)

	var out lockedBuffer
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx, &out) }()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-done)
	})

	return b, core, &out
}

func assertEvent(t *testing.T, want, got *pb.ChatEvent) {
	t.Helper()
	assert.True(t, proto.Equal(want, got), "want %v\ngot  %v", want, got)
}

func TestCommands(t *testing.T) {
	b, core, _ := newTestBackend(t)
	ctx := t.Context()
	var out bytes.Buffer
	run := func(line string) error { return b.Commands().Dispatch(ctx, &out, line) }

	assert.EqualError(t, run("msg #general alice hi"), "not in channel #general, join it first")

	assert.NoError(t, run("join #general"))
	assertEvent(t, &pb.ChatEvent{Inner: &pb.ChatEvent_JoinChannel{JoinChannel: &pb.JoinChannelChatEvent{
		ChannelId: "#general", DisplayName: "#general",
	}}}, core.next(t))

	source := &pb.ChannelSource{ChannelId: "#general", User: &pb.User{Id: "alice", DisplayName: "alice"}}

	assert.NoError(t, run("msg #general alice **hi**"))
	assertEvent(t, &pb.ChatEvent{Inner: &pb.ChatEvent_Message{Message: &pb.MessageEvent{
		Source:    source,
		RootBlock: seabird.NewBoldBlock(seabird.NewTextBlock("hi")),
	}}}, core.next(t))

	assert.NoError(t, run("cmd #general alice weather 98101"))
	assertEvent(t, &pb.ChatEvent{Inner: &pb.ChatEvent_Command{Command: &pb.CommandEvent{
		Source: source, Command: "weather", Arg: "98101",
	}}}, core.next(t))

	assert.NoError(t, run("pm bob hello"))
	assertEvent(t, &pb.ChatEvent{Inner: &pb.ChatEvent_PrivateMessage{PrivateMessage: &pb.PrivateMessageEvent{
		Source:    &pb.User{Id: "bob", DisplayName: "bob"},
		RootBlock: seabird.NewTextBlock("hello"),
	}}}, core.next(t))

	assert.NoError(t, run("topic #general big news"))
	assertEvent(t, &pb.ChatEvent{Inner: &pb.ChatEvent_ChangeChannel{ChangeChannel: &pb.ChangeChannelChatEvent{
		ChannelId: "#general", DisplayName: "#general", Topic: "big news",
	}}}, core.next(t))

	assert.NoError(t, run("channels"))
	assert.Equal(t, "#general name=\"#general\" topic=\"big news\"\n", out.String())

	assert.NoError(t, run("leave #general"))
	assertEvent(t, &pb.ChatEvent{Inner: &pb.ChatEvent_LeaveChannel{LeaveChannel: &pb.LeaveChannelChatEvent{
		ChannelId: "#general",
	}}}, core.next(t))
	assert.Equal(t, 0, len(b.Channels()))

	assert.EqualError(t, run("respond sometimes"), `unknown mode "sometimes", expected ok, fail, ignore, or delay`)
}

func TestRequests(t *testing.T) {
	success := func(id string) *pb.ChatEvent {
		return &pb.ChatEvent{Id: id, Inner: &pb.ChatEvent_Success{Success: &pb.SuccessChatEvent{}}}
	}

	tests := []struct {
		name string
		req  *pb.ChatRequest
		want *pb.ChatEvent
	}{
		{
			name: "send message",
			req:  &pb.ChatRequest{Id: "1", Inner: &pb.ChatRequest_SendMessage{SendMessage: &pb.SendMessageChatRequest{ChannelId: "#general", Text: "hi"}}},
			want: success("1"),
		},
		{
			name: "join",
			req:  &pb.ChatRequest{Id: "2", Inner: &pb.ChatRequest_JoinChannel{JoinChannel: &pb.JoinChannelChatRequest{ChannelName: "#new"}}},
			want: &pb.ChatEvent{Id: "2", Inner: &pb.ChatEvent_JoinChannel{JoinChannel: &pb.JoinChannelChatEvent{ChannelId: "#new", DisplayName: "#new"}}},
		},
		{
			name: "leave unknown channel",
			req:  &pb.ChatRequest{Id: "3", Inner: &pb.ChatRequest_LeaveChannel{LeaveChannel: &pb.LeaveChannelChatRequest{ChannelId: "#nope"}}},
			want: &pb.ChatEvent{Id: "3", Inner: &pb.ChatEvent_Failed{Failed: &pb.FailedChatEvent{Reason: "not in channel #nope"}}},
		},
		{
			name: "metadata",
			req:  &pb.ChatRequest{Id: "4", Inner: &pb.ChatRequest_Metadata{Metadata: &pb.MetadataChatRequest{}}},
			want: &pb.ChatEvent{Id: "4", Inner: &pb.ChatEvent_Metadata{Metadata: &pb.MetadataChatEvent{Values: map[string]string{"devtools/backend": "fake"}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, core, _ := newTestBackend(t)
			core.requests <- tt.req
			assertEvent(t, tt.want, core.next(t))
		})
	}
}

func TestRequestOutput(t *testing.T) {
	_, core, out := newTestBackend(t)
	core.requests <- &pb.ChatRequest{Id: "1", Inner: &pb.ChatRequest_SendMessage{SendMessage: &pb.SendMessageChatRequest{ChannelId: "#general", Text: "hi"}}}
	core.next(t)
	assert.Equal(t, "send_message #general: hi\n", out.String())
}

func TestModes(t *testing.T) {
	req := &pb.ChatRequest{Id: "1", Inner: &pb.ChatRequest_SendMessage{SendMessage: &pb.SendMessageChatRequest{ChannelId: "#general", Text: "hi"}}}

	t.Run("fail", func(t *testing.T) {
		b, core, _ := newTestBackend(t)
		assert.NoError(t, b.Commands().Dispatch(t.Context(), io.Discard, "respond fail rate limited"))
		core.requests <- req
		assertEvent(t, &pb.ChatEvent{Id: "1", Inner: &pb.ChatEvent_Failed{Failed: &pb.FailedChatEvent{Reason: "rate limited"}}}, core.next(t))
	})

	t.Run("ignore", func(t *testing.T) {
		b, core, _ := newTestBackend(t)
		b.SetMode(ModeIgnore, "", 0)
		core.requests <- req
		select {
		case event := <-core.events:
			t.Fatalf("unexpected reply %v", event)
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("delay", func(t *testing.T) {
		b, core, _ := newTestBackend(t)
		assert.NoError(t, b.Commands().Dispatch(t.Context(), io.Discard, "respond delay 200ms"))
		start := time.Now()
		core.requests <- req
		assertEvent(t, &pb.ChatEvent{Id: "1", Inner: &pb.ChatEvent_Success{Success: &pb.SuccessChatEvent{}}}, core.next(t))
		assert.True(t, time.Since(start) >= 200*time.Millisecond)
	})
}

func TestCloseSend(t *testing.T) {
	core := newFakeCore()
	b := &Backend{Type: "dev", ID: "local"}
	assert.NoError(t, b.Connect(t.Context(), dial(t, core)))
	assert.NoError(t, b.Join("#general"))

	done := make(chan error, 1)
	go func() { done <- b.Run(t.Context(), io.Discard) }()

	// Once core sees the half-close, it ends the stream and Run returns, with
	// everything sent before it delivered.
	assert.NoError(t, b.CloseSend())
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after CloseSend")
	}
	assert.Equal(t, 2, len(core.events))
}
