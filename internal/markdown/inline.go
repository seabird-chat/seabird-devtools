package markdown

import (
	"regexp"
	"strconv"
	"time"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// delimiterParser parses text wrapped in a doubled character, like ~~this~~.
// It is a generalized port of goldmark's strikethrough extension.
type delimiterParser struct {
	char      byte
	processor *delimiterProcessor
}

func newDelimiterParser(char byte, kind string) *delimiterParser {
	return &delimiterParser{
		char:      char,
		processor: &delimiterProcessor{char: char, kind: ast.NewNodeKind(kind)},
	}
}

func (p *delimiterParser) Trigger() []byte {
	return []byte{p.char}
}

func (p *delimiterParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()
	if len(line) < 2 || line[0] != p.char || line[1] != p.char {
		return nil
	}

	// Don't start a new delimiter directly after one for the same character,
	// so runs like ___ are left to the emphasis parser.
	if last := pc.LastDelimiter(); last != nil && last.Char == p.char {
		if _, cur := block.Position(); cur.Start == last.Segment.Stop {
			return nil
		}
	}

	d := parser.NewDelimiter(true, true, 2, p.char, p.processor)
	d.Segment = segment.WithStop(segment.Start + 2)
	block.Advance(2)
	pc.PushDelimiter(d)
	return d
}

type delimiterProcessor struct {
	char byte
	kind ast.NodeKind
}

func (p *delimiterProcessor) IsDelimiter(b byte) bool {
	return b == p.char
}

func (p *delimiterProcessor) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char && opener.Length == closer.Length
}

func (p *delimiterProcessor) OnMatch(consumes int) ast.Node {
	return &delimiterNode{char: p.char, kind: p.kind}
}

type delimiterNode struct {
	ast.BaseInline

	char byte
	kind ast.NodeKind
}

func (n *delimiterNode) Kind() ast.NodeKind { return n.kind }

func (n *delimiterNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Char": string(n.char)}, nil)
}

// timestampPattern matches Discord's timestamp syntax. The optional style
// suffix is accepted but dropped, as timestamp blocks don't carry one.
var timestampPattern = regexp.MustCompile(`^<t:(-?\d+)(?::[tTdDfFR])?>`)

var kindTimestamp = ast.NewNodeKind("Timestamp")

// timestampParser parses <t:UNIX_SECONDS> into a timestamp.
type timestampParser struct{}

func (timestampParser) Trigger() []byte {
	return []byte{'<'}
}

func (timestampParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	m := timestampPattern.FindSubmatch(line)
	if m == nil {
		return nil
	}

	secs, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		return nil
	}

	block.Advance(len(m[0]))
	return &timestampNode{time: time.Unix(secs, 0)}
}

type timestampNode struct {
	ast.BaseInline

	time time.Time
}

func (n *timestampNode) Kind() ast.NodeKind { return kindTimestamp }

func (n *timestampNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Time": n.time.String()}, nil)
}
