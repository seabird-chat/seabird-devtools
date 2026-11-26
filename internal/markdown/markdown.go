// Package markdown converts Discord-flavored markdown into seabird message
// blocks.
//
// On top of CommonMark emphasis, links, headings, lists, blockquotes, and code,
// it supports __underline__, ~~strikethrough~~, ||spoilers||, bare URLs, and
// <t:UNIX_SECONDS> timestamps.
package markdown

import (
	"fmt"
	"strings"

	seabird "github.com/seabird-chat/seabird-go"
	"github.com/seabird-chat/seabird-go/pb"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var mdParser = parser.NewParser(
	parser.WithBlockParsers(
		util.Prioritized(parser.NewListParser(), 300),
		util.Prioritized(parser.NewListItemParser(), 400),
		util.Prioritized(parser.NewATXHeadingParser(), 600),
		util.Prioritized(parser.NewFencedCodeBlockParser(), 700),
		util.Prioritized(parser.NewBlockquoteParser(), 800),
		util.Prioritized(parser.NewParagraphParser(), 1000),
	),
	parser.WithInlineParsers(
		util.Prioritized(parser.NewCodeSpanParser(), 100),
		util.Prioritized(parser.NewLinkParser(), 200),
		util.Prioritized(timestampParser{}, 250),
		util.Prioritized(parser.NewAutoLinkParser(), 300),
		// Underline must run before emphasis so __ isn't parsed as bold.
		util.Prioritized(newDelimiterParser('_', "Underline"), 450),
		util.Prioritized(parser.NewEmphasisParser(), 500),
		util.Prioritized(newDelimiterParser('~', "Strikethrough"), 1000),
		util.Prioritized(newDelimiterParser('|', "Spoiler"), 1000),
		util.Prioritized(extension.NewLinkifyParser(), 1000),
	),
)

// Parse converts markdown into a block tree.
func Parse(s string) (*pb.Block, error) {
	src := []byte(s)
	doc := mdParser.Parse(text.NewReader(src))
	return convert(doc, src)
}

func convert(n ast.Node, src []byte) (*pb.Block, error) {
	switch n := n.(type) {
	case *ast.Document, *ast.Paragraph, *ast.TextBlock, *ast.ListItem:
		return children(n, src, maybeContainer)

	case *ast.Text:
		value := n.Segment.Value(src)
		if !n.IsRaw() {
			value = util.UnescapePunctuations(value)
		}
		if n.SoftLineBreak() || n.HardLineBreak() {
			value = append(value, '\n')
		}
		return seabird.NewTextBlock(string(value)), nil
	case *ast.String:
		return seabird.NewTextBlock(string(n.Value)), nil

	case *ast.CodeSpan:
		var b strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			t, ok := c.(*ast.Text)
			if !ok {
				return nil, fmt.Errorf("code span contains %s", c.Kind())
			}
			b.Write(t.Segment.Value(src))
		}
		return seabird.NewInlineCodeBlock(b.String()), nil
	case *ast.FencedCodeBlock:
		var b strings.Builder
		for i := range n.Lines().Len() {
			line := n.Lines().At(i)
			b.Write(line.Value(src))
		}
		return seabird.NewFencedCodeBlock(string(n.Language(src)), strings.TrimSuffix(b.String(), "\n")), nil

	case *ast.Emphasis:
		if n.Level == 2 {
			return children(n, src, seabird.NewBoldBlock)
		}
		return children(n, src, seabird.NewItalicsBlock)
	case *delimiterNode:
		switch n.char {
		case '_':
			return children(n, src, seabird.NewUnderlineBlock)
		case '~':
			return children(n, src, seabird.NewStrikethroughBlock)
		case '|':
			return children(n, src, seabird.NewSpoilerBlock)
		}
		return nil, fmt.Errorf("unknown delimiter %q", n.char)

	case *ast.Link:
		return children(n, src, func(inner ...*pb.Block) *pb.Block {
			return seabird.NewLinkBlock(string(n.Destination), inner...)
		})
	case *ast.AutoLink:
		return seabird.NewLinkBlock(string(n.URL(src)), seabird.NewTextBlock(string(n.Label(src)))), nil

	case *ast.Heading:
		return children(n, src, func(inner ...*pb.Block) *pb.Block {
			return seabird.NewHeadingBlock(n.Level, inner...)
		})
	case *ast.Blockquote:
		return children(n, src, seabird.NewBlockquoteBlock)
	case *ast.List:
		return children(n, src, seabird.NewListBlock)

	case *timestampNode:
		return seabird.NewTimestampBlock(n.time), nil
	}

	return nil, fmt.Errorf("unsupported markdown: %s", n.Kind())
}

// children converts the children of n and passes them to wrap. List items are
// kept separate. Otherwise, block-level siblings are separated by newlines and
// adjacent text is merged.
func children(n ast.Node, src []byte, wrap func(...*pb.Block) *pb.Block) (*pb.Block, error) {
	if n.Kind() == ast.KindList {
		var items []*pb.Block
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			b, err := convert(c, src)
			if err != nil {
				return nil, err
			}
			items = append(items, b)
		}
		return wrap(items...), nil
	}

	var blocks []*pb.Block
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Type() == ast.TypeBlock && c.PreviousSibling() != nil {
			blocks = appendBlock(blocks, seabird.NewTextBlock("\n"))
		}

		b, err := convert(c, src)
		if err != nil {
			return nil, err
		}
		blocks = appendBlock(blocks, b)
	}
	return wrap(blocks...), nil
}

// appendBlock appends b to blocks, merging it into the last block if both are
// text.
func appendBlock(blocks []*pb.Block, b *pb.Block) []*pb.Block {
	if len(blocks) > 0 {
		last, ok := blocks[len(blocks)-1].GetInner().(*pb.Block_Text)
		next, nextOK := b.GetInner().(*pb.Block_Text)
		if ok && nextOK {
			blocks[len(blocks)-1] = seabird.NewTextBlock(last.Text.GetText() + next.Text.GetText())
			return blocks
		}
	}
	return append(blocks, b)
}

// maybeContainer avoids wrapping a single block in a container.
func maybeContainer(blocks ...*pb.Block) *pb.Block {
	if len(blocks) == 1 {
		return blocks[0]
	}
	return seabird.NewContainerBlock(blocks...)
}
