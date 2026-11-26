package format

import (
	"strconv"
	"strings"

	"github.com/seabird-chat/seabird-go/pb"
)

// Standard ANSI foreground colors.
const (
	colorGreen  = 32
	colorYellow = 33
	colorBlue   = 34
	colorCyan   = 36
)

// ANSI renders a block tree with terminal escape codes, similar to how an IRC
// client shows formatting. Multi-line blocks such as lists, quotes, and fenced
// code start on their own line.
func ANSI(b *pb.Block) string {
	r := ansiRenderer{lineStart: true}
	r.render(b)
	return r.b.String()
}

type style struct {
	bold, italic, underline, strike, reverse bool

	// color is an ANSI foreground color, or 0 for the default.
	color int
}

// codes returns the escape sequence which resets the terminal to s.
func (s style) codes() string {
	codes := []string{"0"}
	for _, attr := range []struct {
		on   bool
		code string
	}{
		{s.bold, "1"},
		{s.italic, "3"},
		{s.underline, "4"},
		{s.reverse, "7"},
		{s.strike, "9"},
	} {
		if attr.on {
			codes = append(codes, attr.code)
		}
	}
	if s.color != 0 {
		codes = append(codes, strconv.Itoa(s.color))
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

type ansiRenderer struct {
	b     strings.Builder
	style style

	// prefixes are written after every newline, such as "> " in a quote.
	prefixes []prefix

	// lineStart is true when nothing but prefixes is on the current line.
	lineStart bool

	// pendingBreak is set after a multi-line block, so any following content
	// starts on a new line.
	pendingBreak bool
}

func (r *ansiRenderer) render(b *pb.Block) {
	switch inner := b.GetInner().(type) {
	case *pb.Block_Text:
		r.text(inner.Text.GetText())
	case *pb.Block_Container:
		for _, child := range inner.Container.GetInner() {
			r.render(child)
		}

	case *pb.Block_Bold:
		r.with(func(s *style) { s.bold = true }, inner.Bold.GetInner())
	case *pb.Block_Italics:
		r.with(func(s *style) { s.italic = true }, inner.Italics.GetInner())
	case *pb.Block_Underline:
		r.with(func(s *style) { s.underline = true }, inner.Underline.GetInner())
	case *pb.Block_Strikethrough:
		r.with(func(s *style) { s.strike = true }, inner.Strikethrough.GetInner())
	case *pb.Block_Spoiler:
		r.with(func(s *style) { s.reverse = true }, inner.Spoiler.GetInner())
	case *pb.Block_InlineCode:
		r.styled(func(s *style) { s.color = colorCyan }, func() { r.text(inner.InlineCode.GetText()) })
	case *pb.Block_Timestamp:
		t := inner.Timestamp.GetInner().AsTime().Local().Format("2006-01-02 15:04:05 MST")
		r.styled(func(s *style) { s.color = colorYellow }, func() { r.text(t) })

	case *pb.Block_Link:
		link := inner.Link
		r.with(func(s *style) { s.underline, s.color = true, colorBlue }, link.GetInner())
		if link.GetInner().GetPlain() != link.GetUrl() {
			r.text(" (" + link.GetUrl() + ")")
		}

	case *pb.Block_FencedCode:
		r.block(func() {
			r.styled(func(s *style) { s.color = colorCyan }, func() { r.text(inner.FencedCode.GetText()) })
		})
	case *pb.Block_Heading:
		heading := inner.Heading
		r.block(func() {
			r.styled(func(s *style) { s.bold = true }, func() {
				r.text(strings.Repeat("#", int(heading.GetLevel())) + " ")
				r.render(heading.GetInner())
			})
		})
	case *pb.Block_Blockquote:
		r.block(func() {
			quote := prefix{text: "> ", color: colorGreen}
			r.writePrefixes(quote)
			r.indented(quote, func() { r.render(inner.Blockquote.GetInner()) })
		})
	case *pb.Block_List:
		r.block(func() {
			for _, item := range inner.List.GetInner() {
				if !r.lineStart {
					r.newline()
				}
				r.b.WriteString("• ")
				r.indented(prefix{text: "  "}, func() { r.render(item) })
			}
		})

	default:
		r.text(b.GetPlain())
	}
}

// text writes s, starting a new line for each newline in it.
func (r *ansiRenderer) text(s string) {
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			r.newline()
		}
		if line == "" {
			continue
		}
		if r.pendingBreak {
			r.newline()
		}
		r.b.WriteString(line)
		r.lineStart = false
	}
}

func (r *ansiRenderer) newline() {
	r.b.WriteString("\n")
	r.writePrefixes(r.prefixes...)
	r.lineStart, r.pendingBreak = true, false
}

// prefix is written at the start of each line inside a block.
type prefix struct {
	text  string
	color int
}

// writePrefixes writes prefixes, then restores the current style if any of
// them changed it.
func (r *ansiRenderer) writePrefixes(prefixes ...prefix) {
	var styled bool
	for _, p := range prefixes {
		if p.color != 0 {
			r.b.WriteString(style{color: p.color}.codes())
			styled = true
		}
		r.b.WriteString(p.text)
	}
	if styled {
		r.b.WriteString(r.style.codes())
	}
}

// with renders b with the style changed by apply.
func (r *ansiRenderer) with(apply func(*style), b *pb.Block) {
	r.styled(apply, func() { r.render(b) })
}

// styled runs fn with the style changed by apply, restoring it afterward.
func (r *ansiRenderer) styled(apply func(*style), fn func()) {
	prev := r.style
	apply(&r.style)
	r.b.WriteString(r.style.codes())
	fn()
	r.style = prev
	r.b.WriteString(prev.codes())
}

// block runs fn on its own line, and makes any following content start on a
// new line.
func (r *ansiRenderer) block(fn func()) {
	if !r.lineStart {
		r.newline()
	}
	fn()
	r.pendingBreak = true
}

// indented runs fn with p written after every newline.
func (r *ansiRenderer) indented(p prefix, fn func()) {
	r.prefixes = append(r.prefixes, p)
	fn()
	r.prefixes = r.prefixes[:len(r.prefixes)-1]
}
