package mcp

import (
	"strings"
	"testing"
)

func TestSSEEvents_Table(t *testing.T) {
	tests := []struct {
		name, stream string
		want         []string
	}{
		{"single", "data: {\"a\":1}\n\n", []string{`{"a":1}`}},
		{"multi line data", "data: {\"a\":\ndata: 1}\n\n", []string{"{\"a\":\n1}"}},
		{"comment and other fields", ": hi\nid: 7\nevent: message\nretry: 10\ndata: x\n\n", []string{"x"}},
		{"priming event skipped", "id: 1\ndata: \n\ndata: y\n\n", []string{"y"}},
		{"no trailing blank line", "data: z", []string{"z"}},
		{"crlf", "data: q\r\n\r\n", []string{"q"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			if err := sseEvents(strings.NewReader(tt.stream), func(d []byte) { got = append(got, string(d)) }); err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
