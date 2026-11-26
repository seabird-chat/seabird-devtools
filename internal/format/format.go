// Package format renders seabird protocol messages as single lines of text.
package format

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/seabird-chat/seabird-go/pb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// JSON renders msg as a single line of protojson.
func JSON(msg proto.Message) string {
	b, err := protojson.Marshal(msg)
	if err != nil {
		return fmt.Sprintf("error: marshal %T: %v", msg, err)
	}
	return string(b)
}

// Event renders an event from seabird-core's event stream.
func Event(e *pb.Event) string {
	var s string
	switch inner := e.GetInner().(type) {
	case *pb.Event_Message:
		m := inner.Message
		s = fmt.Sprintf("message %s %s: %s", m.GetSource().GetChannelId(), User(m.GetSource().GetUser()), m.GetText())
	case *pb.Event_PrivateMessage:
		m := inner.PrivateMessage
		s = fmt.Sprintf("private_message %s: %s", User(m.GetSource()), m.GetText())
	case *pb.Event_Mention:
		m := inner.Mention
		s = fmt.Sprintf("mention %s %s: %s", m.GetSource().GetChannelId(), User(m.GetSource().GetUser()), m.GetText())
	case *pb.Event_Command:
		m := inner.Command
		s = fmt.Sprintf("command %s %s: %s %s", m.GetSource().GetChannelId(), User(m.GetSource().GetUser()), m.GetCommand(), m.GetArg())
	case *pb.Event_Action:
		m := inner.Action
		s = fmt.Sprintf("action %s %s: %s", m.GetSource().GetChannelId(), User(m.GetSource().GetUser()), m.GetText())
	case *pb.Event_PrivateAction:
		m := inner.PrivateAction
		s = fmt.Sprintf("private_action %s: %s", User(m.GetSource()), m.GetText())
	case *pb.Event_SendMessage:
		m := inner.SendMessage
		s = fmt.Sprintf("send_message %s from %s: %s", m.GetChannelId(), m.GetSender(), m.GetText())
	case *pb.Event_SendPrivateMessage:
		m := inner.SendPrivateMessage
		s = fmt.Sprintf("send_private_message %s from %s: %s", m.GetUserId(), m.GetSender(), m.GetText())
	case *pb.Event_PerformAction:
		m := inner.PerformAction
		s = fmt.Sprintf("perform_action %s from %s: %s", m.GetChannelId(), m.GetSender(), m.GetText())
	case *pb.Event_PerformPrivateAction:
		m := inner.PerformPrivateAction
		s = fmt.Sprintf("perform_private_action %s from %s: %s", m.GetUserId(), m.GetSender(), m.GetText())
	default:
		s = "unknown event " + JSON(e)
	}

	return strings.TrimRight(s, " ") + Tags(e.GetTags())
}

// User renders a user as its display name followed by its ID.
func User(u *pb.User) string {
	return fmt.Sprintf("%s(%s)", u.GetDisplayName(), u.GetId())
}

// Tags renders tags sorted by key, with a leading space, or an empty string
// when there are none.
func Tags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}

	pairs := make([]string, 0, len(tags))
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		pairs = append(pairs, k+"="+tags[k])
	}
	return " [" + strings.Join(pairs, " ") + "]"
}

// Backend renders a backend as its ID and type.
func Backend(b *pb.Backend) string {
	return fmt.Sprintf("%s type=%s", b.GetId(), b.GetType())
}

// Channel renders a channel as its ID, display name, and topic.
func Channel(c *pb.Channel) string {
	return fmt.Sprintf("%s name=%q topic=%q", c.GetId(), c.GetDisplayName(), c.GetTopic())
}

// Command renders a command's metadata on one line, leaving out empty help.
func Command(c *pb.CommandMetadata) string {
	s := c.GetName()
	if short := c.GetShortHelp(); short != "" {
		s += " " + short
	}
	if full := c.GetFullHelp(); full != "" {
		s += ": " + full
	}
	return s
}
