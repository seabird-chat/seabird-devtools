package format

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	seabird "github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
)

func TestEvent(t *testing.T) {
	source := &pb.ChannelSource{
		ChannelId: "sbc_chan",
		User:      &pb.User{Id: "sbu_alice", DisplayName: "alice"},
	}
	alice := source.GetUser()

	tests := []struct {
		name  string
		event *pb.Event
		color bool
		want  string
	}{
		{
			name:  "message",
			event: &pb.Event{Inner: &pb.Event_Message{Message: &pb.MessageEvent{Source: source, Text: "hi"}}},
			want:  "message sbc_chan alice(sbu_alice): hi",
		},
		{
			name:  "private message",
			event: &pb.Event{Inner: &pb.Event_PrivateMessage{PrivateMessage: &pb.PrivateMessageEvent{Source: alice, Text: "hi"}}},
			want:  "private_message alice(sbu_alice): hi",
		},
		{
			name:  "command",
			event: &pb.Event{Inner: &pb.Event_Command{Command: &pb.CommandEvent{Source: source, Command: "weather", Arg: "98101"}}},
			want:  "command sbc_chan alice(sbu_alice): weather 98101",
		},
		{
			name:  "command without arg",
			event: &pb.Event{Inner: &pb.Event_Command{Command: &pb.CommandEvent{Source: source, Command: "ping"}}},
			want:  "command sbc_chan alice(sbu_alice): ping",
		},
		{
			name: "send message with tags",
			event: &pb.Event{
				Inner: &pb.Event_SendMessage{SendMessage: &pb.SendMessageEvent{Sender: "url-plugin", ChannelId: "sbc_chan", Text: "title"}},
				Tags:  map[string]string{"seabird/link-preview": "1", "a": "b"},
			},
			want: "send_message sbc_chan from url-plugin: title [a=b seabird/link-preview=1]",
		},
		{
			name:  "perform private action",
			event: &pb.Event{Inner: &pb.Event_PerformPrivateAction{PerformPrivateAction: &pb.PerformPrivateActionEvent{Sender: "p", UserId: "sbu_alice", Text: "waves"}}},
			want:  "perform_private_action sbu_alice from p: waves",
		},
		{
			name: "message with color",
			event: &pb.Event{Inner: &pb.Event_Message{Message: &pb.MessageEvent{
				Source:    source,
				Text:      "hi",
				RootBlock: seabird.NewBoldBlock(seabird.NewTextBlock("hi")),
			}}},
			color: true,
			want:  "message sbc_chan alice(sbu_alice): \x1b[0;1mhi\x1b[0m",
		},
		{
			name:  "color without blocks",
			event: &pb.Event{Inner: &pb.Event_Message{Message: &pb.MessageEvent{Source: source, Text: "hi"}}},
			color: true,
			want:  "message sbc_chan alice(sbu_alice): hi",
		},
		{
			name:  "unknown",
			event: &pb.Event{},
			want:  "unknown event {}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Event(tt.event, tt.color))
		})
	}
}

func TestChatRequest(t *testing.T) {
	tests := []struct {
		name string
		req  *pb.ChatRequest
		want string
	}{
		{
			name: "send message",
			req: &pb.ChatRequest{Inner: &pb.ChatRequest_SendMessage{SendMessage: &pb.SendMessageChatRequest{
				ChannelId: "#general",
				Text:      "hi",
				Tags:      map[string]string{"a": "b"},
			}}},
			want: "send_message #general: hi [a=b]",
		},
		{
			name: "private action",
			req:  &pb.ChatRequest{Inner: &pb.ChatRequest_PerformPrivateAction{PerformPrivateAction: &pb.PerformPrivateActionChatRequest{UserId: "alice", Text: "waves"}}},
			want: "perform_private_action alice: waves",
		},
		{
			name: "join",
			req:  &pb.ChatRequest{Inner: &pb.ChatRequest_JoinChannel{JoinChannel: &pb.JoinChannelChatRequest{ChannelName: "#new"}}},
			want: "join_channel #new",
		},
		{
			name: "update channel info",
			req:  &pb.ChatRequest{Inner: &pb.ChatRequest_UpdateChannelInfo{UpdateChannelInfo: &pb.UpdateChannelInfoChatRequest{ChannelId: "#general", Topic: "news"}}},
			want: `update_channel_info #general topic="news"`,
		},
		{
			name: "metadata",
			req:  &pb.ChatRequest{Inner: &pb.ChatRequest_Metadata{Metadata: &pb.MetadataChatRequest{}}},
			want: "metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ChatRequest(tt.req, false))
		})
	}
}
