package markdown

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/seabird-chat/seabird-go/pb"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "hello", want: `"hello"`},
		{in: "a *b* _c_ **d**", want: `container("a " italics("b") " " italics("c") " " bold("d"))`},
		{in: "__u__ ~~s~~ ||x||", want: `container(underline("u") " " strikethrough("s") " " spoiler("x"))`},
		{in: "***both***", want: `italics(bold("both"))`},
		{in: "`a *b*`", want: "code(`a *b*`)"},
		{in: "```go\nfmt.Println()\n```", want: "fence(go, `fmt.Println()`)"},
		{in: "[site](https://example.com)", want: `link(https://example.com, "site")`},
		{in: "see https://example.com", want: `container("see " link(https://example.com, "https://example.com"))`},
		{in: "<https://example.com>", want: `link(https://example.com, "https://example.com")`},
		{in: "# title", want: `heading(1, "title")`},
		{in: "> quoted", want: `quote("quoted")`},
		{in: "- a\n- *b*", want: `list("a" italics("b"))`},
		{in: "- a\n- b", want: `list("a" "b")`},
		{in: "> one\n>\n> two", want: `quote("one\ntwo")`},
		{in: "at <t:1700000000> or <t:1700000000:R>", want: `container("at " time(1700000000) " or " time(1700000000))`},
		{in: "<t:nope>", want: `"<t:nope>"`},
		{in: "line one\nline two", want: `"line one\nline two"`},
		{in: "para\n\n# head", want: `container("para\n" heading(1, "head"))`},
		{in: `not \*italic\*`, want: `"not *italic*"`},
		{in: "under_score_word", want: `"under_score_word"`},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			b, err := Parse(tt.in)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, dump(b))
		})
	}
}

func TestParseUnsupported(t *testing.T) {
	_, err := Parse("![image](https://example.com/a.png)")
	assert.EqualError(t, err, "unsupported markdown: Image")
}

// dump renders a block tree compactly for comparison.
func dump(b *pb.Block) string {
	switch inner := b.GetInner().(type) {
	case *pb.Block_Text:
		return fmt.Sprintf("%q", inner.Text.GetText())
	case *pb.Block_InlineCode:
		return "code(`" + inner.InlineCode.GetText() + "`)"
	case *pb.Block_FencedCode:
		return fmt.Sprintf("fence(%s, `%s`)", inner.FencedCode.GetInfo(), inner.FencedCode.GetText())
	case *pb.Block_Timestamp:
		return fmt.Sprintf("time(%d)", inner.Timestamp.GetInner().GetSeconds())
	case *pb.Block_Italics:
		return "italics(" + dump(inner.Italics.GetInner()) + ")"
	case *pb.Block_Bold:
		return "bold(" + dump(inner.Bold.GetInner()) + ")"
	case *pb.Block_Underline:
		return "underline(" + dump(inner.Underline.GetInner()) + ")"
	case *pb.Block_Strikethrough:
		return "strikethrough(" + dump(inner.Strikethrough.GetInner()) + ")"
	case *pb.Block_Spoiler:
		return "spoiler(" + dump(inner.Spoiler.GetInner()) + ")"
	case *pb.Block_Blockquote:
		return "quote(" + dump(inner.Blockquote.GetInner()) + ")"
	case *pb.Block_Heading:
		return fmt.Sprintf("heading(%d, %s)", inner.Heading.GetLevel(), dump(inner.Heading.GetInner()))
	case *pb.Block_Link:
		return fmt.Sprintf("link(%s, %s)", inner.Link.GetUrl(), dump(inner.Link.GetInner()))
	case *pb.Block_List:
		return "list(" + dumpAll(inner.List.GetInner()) + ")"
	case *pb.Block_Container:
		return "container(" + dumpAll(inner.Container.GetInner()) + ")"
	}
	return fmt.Sprintf("unknown(%T)", b.GetInner())
}

func dumpAll(blocks []*pb.Block) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		parts = append(parts, dump(b))
	}
	return strings.Join(parts, " ")
}
