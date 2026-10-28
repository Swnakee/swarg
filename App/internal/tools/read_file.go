package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// read_file
// ---------------------------------------------------------------------------

const (
	defaultReadLimit = 200
	binaryProbe      = 8192 // bytes checked for NUL to detect a binary file
)

// ReadFile returns numbered lines of a text file inside the project.
type ReadFile struct{}

// Spec describes read_file for the model.
func (ReadFile) Spec() chat.ToolSpec {
	return chat.ToolSpec{
		Name:        "read_file",
		Description: "Read a text file, lines numbered from 1.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string","description":"file path"},` +
			`"offset":{"type":"integer","description":"first line (default 1)"},` +
			`"limit":{"type":"integer","description":"max lines (default 200)"}},` +
			`"required":["path"]}`),
	}
}

// ReadOnly is true: read_file never changes files.
func (ReadFile) ReadOnly() bool { return true }

// Confirm is false: read_file runs without approval.
func (ReadFile) Confirm() bool { return false }

// Block is the system prompt text for read_file.
func (ReadFile) Block() string {
	return "read_file: read a text file before you change or explain it. Lines come back as \"N: text\". Leading / = project root.\n" +
		`Example: {"path": "main.go", "offset": 1, "limit": 100}`
}

// Summary is the path, or "" when the args are not valid JSON.
func (ReadFile) Summary(args json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	return a.Path
}

// Run returns the requested line range. It fails on directories, binary files and
// offsets past the end; a range that stops short of the end gets a continuation note.
func (t ReadFile) Run(_ context.Context, env Env, args json.RawMessage) (Result, error) {
	var a struct {
		Path   string `json:"path"`
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if err := DecodeFor(t, args, &a, "path"); err != nil {
		return Result{}, err
	}
	offset, limit := 1, defaultReadLimit
	if a.Offset != nil {
		offset = *a.Offset
	}
	if a.Limit != nil {
		limit = *a.Limit
	}
	if offset <= 0 || limit <= 0 {
		return Result{}, errors.New("offset and limit must be >= 1")
	}
	rel, err := Rel(env.Dir, a.Path)
	if err != nil {
		return Result{}, err
	}
	f, err := env.Root.Open(rel)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Result{}, err
	}
	if fi.IsDir() {
		return Result{}, fmt.Errorf("%s is a directory; use list_dir", a.Path)
	}
	head := make([]byte, binaryProbe)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return Result{}, err
	}
	head = head[:n]
	if isBinary(head) {
		return Result{}, errors.New("binary file (NUL byte in the first 8 KB)")
	}

	// Stream the whole file (to count lines) but keep only the requested range.
	br := bufio.NewReader(io.MultiReader(bytes.NewReader(head), f))
	var out []string
	total, last := 0, offset+limit-1
	for {
		line, rerr := br.ReadString('\n')
		if rerr != nil && rerr != io.EOF {
			return Result{}, rerr
		}
		if rerr == io.EOF && line == "" {
			break
		}
		total++
		if total >= offset && total <= last {
			text := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			out = append(out, fmt.Sprintf("%d: %s", total, text))
		}
		if rerr == io.EOF {
			break
		}
	}
	if total > 0 && offset > total {
		return Result{}, fmt.Errorf("offset %d is past the end of the file (%d lines)", offset, total)
	}
	shown := len(out)
	if total > last {
		out = append(out, fmt.Sprintf("[... %d more lines; use offset %d]", total-last, last+1))
	}
	return Result{Output: strings.Join(out, "\n"), Brief: fmt.Sprintf("%d lines", shown)}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isBinary reports whether the first 8 KB of b contain a NUL byte.
func isBinary(b []byte) bool {
	if len(b) > binaryProbe {
		b = b[:binaryProbe]
	}
	return bytes.IndexByte(b, 0) >= 0
}
