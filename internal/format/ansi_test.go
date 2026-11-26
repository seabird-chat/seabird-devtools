package format

import (
	"regexp"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/seabird-chat/seabird-go/pb"

	"github.com/seabird-chat/seabird-devtools/internal/markdown"
)

var escapePattern = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// readable replaces escape codes with {codes} so expectations are legible.
func readable(s string) string {
	return escapePattern.ReplaceAllString(s, "{$1}")
}

func TestANSI(t *testing.T) {
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })

	tests := []struct {
		in   string
		want string
	}{
		{in: "plain", want: "plain"},
		{in: "a **b** c", want: "a {0;1}b{0} c"},
		{in: "*i* __u__ ~~s~~ ||x||", want: "{0;3}i{0} {0;4}u{0} {0;9}s{0} {0;7}x{0}"},
		{in: "**bold *both***", want: "{0;1}bold {0;1;3}both{0;1}{0}"},
		{in: "run `ls -l`", want: "run {0;36}ls -l{0}"},
		{in: "[site](https://x.y)", want: "{0;4;34}site{0} (https://x.y)"},
		{in: "https://x.y", want: "{0;4;34}https://x.y{0}"},
		{in: "at <t:1700000000>", want: "at {0;33}2023-11-14 22:13:20 UTC{0}"},
		{in: "## title", want: "{0;1}## title{0}"},
		{in: "intro\n- a\n- **b**\n\nafter", want: "intro\n• a\n• {0;1}b{0}\nafter"},
		{in: "- a\n  - nested", want: "• a\n  • nested"},
		{in: "> one\n> **two\n> three**", want: "{0;32}> {0}one\n{0;32}> {0}{0;1}two\n{0;32}> {0;1}three{0}"},
		{in: "see\n```go\nx := 1\ny := 2\n```\nok", want: "see\n{0;36}x := 1\ny := 2{0}\nok"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			b, err := markdown.Parse(tt.in)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, readable(ANSI(b)))
		})
	}
}

func TestANSIUnknownBlock(t *testing.T) {
	assert.Equal(t, "fallback", ANSI(&pb.Block{Plain: "fallback"}))
}
