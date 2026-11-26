package repl

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		spec    []string
		line    string
		want    []string
		wantErr string
	}{
		{name: "none", spec: nil, line: "  ", want: []string{}},
		{name: "required", spec: []string{"<a>", "<b>"}, line: " x  y ", want: []string{"x", "y"}},
		{name: "missing", spec: []string{"<a>", "<b>"}, line: "x", wantErr: "missing <b>"},
		{name: "extra", spec: []string{"<a>"}, line: "x y", wantErr: "too many arguments"},
		{name: "rest", spec: []string{"<a>", "<text...>"}, line: "x hello  there ", want: []string{"x", "hello  there"}},
		{name: "rest missing", spec: []string{"<a>", "<text...>"}, line: "x ", wantErr: "missing <text...>"},
		{name: "optional rest", spec: []string{"<a>", "[text...]"}, line: "x", want: []string{"x", ""}},
		{name: "optional", spec: []string{"[a]"}, line: "", want: []string{""}},
		{name: "tabs", spec: []string{"<a>", "<b>"}, line: "x\ty", want: []string{"x", "y"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.spec, tt.line)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDispatch(t *testing.T) {
	var got []string
	commands := Commands{{
		Name: "send",
		Args: []string{"<channel>", "<text...>"},
		Help: "send a message",
		Run: func(ctx context.Context, w io.Writer, args []string) error {
			got = args
			return nil
		},
	}}

	var buf bytes.Buffer
	ctx := t.Context()

	assert.NoError(t, commands.Dispatch(ctx, &buf, "send #chan hi there"))
	assert.Equal(t, []string{"#chan", "hi there"}, got)

	assert.NoError(t, commands.Dispatch(ctx, &buf, "   "))

	err := commands.Dispatch(ctx, &buf, "send #chan")
	assert.EqualError(t, err, "missing <text...> (usage: send <channel> <text...>)")

	err = commands.Dispatch(ctx, &buf, "nope")
	assert.EqualError(t, err, `unknown command "nope", try help`)

	assert.NoError(t, commands.Dispatch(ctx, &buf, "help"))
	assert.Equal(t, "send <channel> <text...>  send a message\nhelp                      list commands\n", buf.String())
}
