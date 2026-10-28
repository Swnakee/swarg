package openai

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// collect reads every event from r with readSSE.
func collect(r io.Reader) ([]sseEvent, error) {
	var got []sseEvent
	err := readSSE(r, func(ev sseEvent) error {
		got = append(got, ev)
		return nil
	})
	return got, err
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestReadSSE_Events(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []sseEvent
	}{
		{"single data line", "data: hello\n\n", []sseEvent{{Data: "hello"}}},
		{"two data lines joined", "data: a\ndata: b\n\n", []sseEvent{{Data: "a\nb"}}},
		{"blank line terminates", "data: a\n\ndata: b\n\n", []sseEvent{{Data: "a"}, {Data: "b"}}},
		{"CRLF line ends", "data: a\r\n\r\n", []sseEvent{{Data: "a"}}},
		{"data without space", "data:x\n\n", []sseEvent{{Data: "x"}}},
		{"data with space", "data: x\n\n", []sseEvent{{Data: "x"}}},
		{"only one space trimmed", "data:  x\n\n", []sseEvent{{Data: " x"}}},
		{"id event retry ignored", "id: 1\nevent: msg\nretry: 10\ndata: a\n\n", []sseEvent{{Data: "a"}}},
		{"unknown field ignored", "foo: bar\ndata: a\n\n", []sseEvent{{Data: "a"}}},
		{"empty data line", "data:\n\n", []sseEvent{{Data: ""}}},
		{"no data no event", "\n\nid: 1\n\n", nil},
		{"last event without blank line", "data: a\n", []sseEvent{{Data: "a"}}},
		{"last line without newline", "data: a", []sseEvent{{Data: "a"}}},
		{"empty input", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect(strings.NewReader(tc.in))
			if err != nil {
				t.Fatalf("readSSE: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("events = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReadSSE_Comments(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []sseEvent
	}{
		{"keepalive comment", ": keepalive 3/18\n", []sseEvent{{Comment: "keepalive 3/18"}}},
		{"bare colon", ":\n", []sseEvent{{Comment: ""}}},
		{"no space after colon", ":x\n", []sseEvent{{Comment: "x"}}},
		{"only one space trimmed", ":  x\n", []sseEvent{{Comment: " x"}}},
		{"CRLF comment", ": x\r\n", []sseEvent{{Comment: "x"}}},
		{
			"comment between data lines does not split the event",
			"data: a\n: c\ndata: b\n\n",
			[]sseEvent{{Comment: "c"}, {Data: "a\nb"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect(strings.NewReader(tc.in))
			if err != nil {
				t.Fatalf("readSSE: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("events = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReadSSE_LongLine(t *testing.T) {
	const size = 1 << 20
	payload := strings.Repeat("x", size)
	got, err := collect(strings.NewReader("data: " + payload + "\n\n"))
	if err != nil {
		t.Fatalf("readSSE: %v", err)
	}
	if len(got) != 1 || got[0].Data != payload {
		t.Errorf("got %d events; want 1 event with %d bytes of data", len(got), size)
	}
}

func TestReadSSE_ChunkBoundaries(t *testing.T) {
	const in = ": keepalive 1/2\r\ndata: привет\r\ndata: мир\r\n\r\nid: 9\ndata: tail"
	whole, err := collect(strings.NewReader(in))
	if err != nil {
		t.Fatalf("whole read: %v", err)
	}
	readers := map[string]func(io.Reader) io.Reader{
		"OneByteReader": iotest.OneByteReader,
		"HalfReader":    iotest.HalfReader,
	}
	for name, wrap := range readers {
		t.Run(name, func(t *testing.T) {
			got, err := collect(wrap(strings.NewReader(in)))
			if err != nil {
				t.Fatalf("readSSE: %v", err)
			}
			if !slices.Equal(got, whole) {
				t.Errorf("events = %+v, want %+v", got, whole)
			}
		})
	}
}

func TestReadSSE_Errors(t *testing.T) {
	errBoom := errors.New("boom")

	t.Run("reader error mid-event", func(t *testing.T) {
		r := io.MultiReader(strings.NewReader("data: a\n"), iotest.ErrReader(errBoom))
		got, err := collect(r)
		if !errors.Is(err, errBoom) {
			t.Fatalf("err = %v, want wrapped %v", err, errBoom)
		}
		if len(got) != 0 {
			t.Errorf("partial event dispatched on read error: %+v", got)
		}
	})

	t.Run("callback error aborts and is returned", func(t *testing.T) {
		calls := 0
		errStop := errors.New("stop")
		err := readSSE(strings.NewReader("data: a\n\ndata: b\n\n"), func(sseEvent) error {
			calls++
			return errStop
		})
		if err != errStop {
			t.Errorf("err = %v, want %v", err, errStop)
		}
		if calls != 1 {
			t.Errorf("callback called %d times, want 1", calls)
		}
	})
}

func FuzzReadSSE(f *testing.F) {
	for _, seed := range []string{
		"",
		"data: a\n\n",
		"data: a\r\ndata: b\r\n\r\n",
		": c\ndata: a\n: d\ndata: b\n\n",
		"data:\n\n",
		"id: 1\nevent: x\nretry: 2\n",
		"data: a",
		"\n\n\n",
		"data: привет\n\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		whole, err := collect(strings.NewReader(in))
		if err != nil {
			t.Fatalf("whole read: %v", err)
		}
		split, err := collect(iotest.OneByteReader(strings.NewReader(in)))
		if err != nil {
			t.Fatalf("byte-wise read: %v", err)
		}
		if !slices.Equal(whole, split) {
			t.Errorf("chunking changed events: %+v vs %+v", whole, split)
		}
	})
}
