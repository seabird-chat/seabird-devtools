// Package backend implements a fake chat backend which emits events typed at
// a prompt and answers seabird-core's requests.
package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/seabird-chat/seabird-go/pb"
	"google.golang.org/protobuf/proto"

	"github.com/seabird-chat/seabird-devtools/internal/format"
)

// Mode is how the backend answers requests from core.
type Mode int

const (
	// ModeOK answers requests the way a working backend would.
	ModeOK Mode = iota
	// ModeFail answers every request with a failure.
	ModeFail
	// ModeIgnore never answers, so core's requests time out.
	ModeIgnore
	// ModeDelay answers like ModeOK, after a delay.
	ModeDelay
)

// Backend is a fake chat backend. Channel and user IDs are the names typed at
// the prompt, such as #general or alice.
type Backend struct {
	// Type and ID identify the backend to core.
	Type string
	ID   string

	// Plain sends message text as-is rather than parsing it as markdown.
	Plain bool

	// Color renders formatting in requests with terminal escape codes.
	Color bool

	// JSON prints requests as protojson rather than text.
	JSON bool

	sendMu sync.Mutex
	stream pb.ChatIngest_IngestEventsClient
	cancel context.CancelFunc

	mu       sync.Mutex
	channels map[string]*pb.Channel
	mode     Mode
	reason   string
	delay    time.Duration
}

// Connect opens the event stream to core and introduces the backend. The
// stream lasts until ctx is done or Close is called.
func (b *Backend) Connect(ctx context.Context, client pb.ChatIngestClient) error {
	ctx, cancel := context.WithCancel(ctx)
	stream, err := client.IngestEvents(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("ingest events: %w", err)
	}
	b.stream, b.cancel = stream, cancel

	return b.send(&pb.ChatEvent{Inner: &pb.ChatEvent_Hello{Hello: &pb.HelloChatEvent{
		BackendInfo: &pb.Backend{Type: b.Type, Id: b.ID},
	}}})
}

// Close ends the stream immediately.
func (b *Backend) Close() {
	if b.cancel != nil {
		b.cancel()
	}
}

// CloseSend tells core no more events are coming. Run returns once core has
// closed its side.
func (b *Backend) CloseSend() error {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()

	if b.stream == nil {
		return nil
	}
	return b.stream.CloseSend()
}

// Run prints and answers requests from core until the stream ends or ctx is
// done, which also closes the stream.
func (b *Backend) Run(ctx context.Context, w io.Writer) error {
	if b.stream == nil {
		return errors.New("not connected")
	}

	stop := context.AfterFunc(ctx, b.cancel)
	defer stop()

	// Delayed replies are sent from goroutines, which must finish before Run
	// returns.
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		req, err := b.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("receive request: %w", err)
		}

		b.print(w, req, format.ChatRequest(req, b.Color))

		mode, delay := b.currentMode()
		switch mode {
		case ModeIgnore:
			continue
		case ModeDelay:
			wg.Go(func() {
				select {
				case <-time.After(delay):
					b.reply(w, req)
				case <-ctx.Done():
				}
			})
		default:
			b.reply(w, req)
		}
	}
}

// SetMode changes how requests are answered. The reason is used by ModeFail
// and the delay by ModeDelay.
func (b *Backend) SetMode(mode Mode, reason string, delay time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.mode, b.reason, b.delay = mode, reason, delay
}

func (b *Backend) currentMode() (Mode, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.mode, b.delay
}

// Join joins a channel and tells core about it.
func (b *Backend) Join(name string) error {
	return b.send(b.join("", name))
}

// Channels returns the joined channels, sorted by ID.
func (b *Backend) Channels() []*pb.Channel {
	b.mu.Lock()
	defer b.mu.Unlock()

	ids := slices.Sorted(maps.Keys(b.channels))
	channels := make([]*pb.Channel, 0, len(ids))
	for _, id := range ids {
		channels = append(channels, proto.CloneOf(b.channels[id]))
	}
	return channels
}

// reply answers a request, printing any error sending it.
func (b *Backend) reply(w io.Writer, req *pb.ChatRequest) {
	if err := b.send(b.answer(req)); err != nil {
		fmt.Fprintf(w, "error: reply to %s: %v\n", req.GetId(), err)
	}
}

// answer builds the reply to a request, updating channel state for joins,
// leaves, and topic changes.
func (b *Backend) answer(req *pb.ChatRequest) *pb.ChatEvent {
	b.mu.Lock()
	mode, reason := b.mode, b.reason
	b.mu.Unlock()

	if mode == ModeFail {
		return failed(req.GetId(), reason)
	}

	switch inner := req.GetInner().(type) {
	case *pb.ChatRequest_JoinChannel:
		return b.join(req.GetId(), inner.JoinChannel.GetChannelName())
	case *pb.ChatRequest_LeaveChannel:
		event, err := b.leave(req.GetId(), inner.LeaveChannel.GetChannelId())
		if err != nil {
			return failed(req.GetId(), err.Error())
		}
		return event
	case *pb.ChatRequest_UpdateChannelInfo:
		info := inner.UpdateChannelInfo
		event, err := b.change(req.GetId(), info.GetChannelId(), func(c *pb.Channel) { c.Topic = info.GetTopic() })
		if err != nil {
			return failed(req.GetId(), err.Error())
		}
		return event
	case *pb.ChatRequest_Metadata:
		return &pb.ChatEvent{Id: req.GetId(), Inner: &pb.ChatEvent_Metadata{Metadata: &pb.MetadataChatEvent{
			Values: map[string]string{"devtools/backend": "fake"},
		}}}
	default:
		return &pb.ChatEvent{Id: req.GetId(), Inner: &pb.ChatEvent_Success{Success: &pb.SuccessChatEvent{}}}
	}
}

func failed(id, reason string) *pb.ChatEvent {
	return &pb.ChatEvent{Id: id, Inner: &pb.ChatEvent_Failed{Failed: &pb.FailedChatEvent{Reason: reason}}}
}

// join records a channel and returns the event announcing it. Joining a
// channel twice announces it again.
func (b *Backend) join(id, name string) *pb.ChatEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	channel, ok := b.channels[name]
	if !ok {
		channel = &pb.Channel{Id: name, DisplayName: name}
		if b.channels == nil {
			b.channels = make(map[string]*pb.Channel)
		}
		b.channels[name] = channel
	}

	return &pb.ChatEvent{Id: id, Inner: &pb.ChatEvent_JoinChannel{JoinChannel: &pb.JoinChannelChatEvent{
		ChannelId:   channel.GetId(),
		DisplayName: channel.GetDisplayName(),
		Topic:       channel.GetTopic(),
	}}}
}

func (b *Backend) leave(id, channelID string) (*pb.ChatEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.channels[channelID]; !ok {
		return nil, fmt.Errorf("not in channel %s", channelID)
	}
	delete(b.channels, channelID)

	return &pb.ChatEvent{Id: id, Inner: &pb.ChatEvent_LeaveChannel{LeaveChannel: &pb.LeaveChannelChatEvent{
		ChannelId: channelID,
	}}}, nil
}

// change updates a joined channel and returns the event announcing it.
func (b *Backend) change(id, channelID string, update func(*pb.Channel)) (*pb.ChatEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	channel, ok := b.channels[channelID]
	if !ok {
		return nil, fmt.Errorf("not in channel %s", channelID)
	}
	update(channel)

	return &pb.ChatEvent{Id: id, Inner: &pb.ChatEvent_ChangeChannel{ChangeChannel: &pb.ChangeChannelChatEvent{
		ChannelId:   channel.GetId(),
		DisplayName: channel.GetDisplayName(),
		Topic:       channel.GetTopic(),
	}}}, nil
}

// joined returns an error unless channelID has been joined.
func (b *Backend) joined(channelID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.channels[channelID]; !ok {
		return fmt.Errorf("not in channel %s, join it first", channelID)
	}
	return nil
}

func (b *Backend) send(event *pb.ChatEvent) error {
	b.sendMu.Lock()
	defer b.sendMu.Unlock()

	if b.stream == nil {
		return errors.New("not connected")
	}
	if err := b.stream.Send(event); err != nil {
		return fmt.Errorf("send event: %w", err)
	}
	return nil
}

func (b *Backend) print(w io.Writer, msg proto.Message, text string) {
	if b.JSON {
		text = format.JSON(msg)
	}
	fmt.Fprintln(w, text)
}
