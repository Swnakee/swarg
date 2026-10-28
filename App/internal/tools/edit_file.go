package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"swarg/internal/chat"
)

// EditFile replaces one exact fragment of a file inside the project folder.
type EditFile struct{}

// ---------------------------------------------------------------------------
// Tool
// ---------------------------------------------------------------------------

// Spec describes edit_file for the model.
func (EditFile) Spec() chat.ToolSpec {
	return chat.ToolSpec{
		Name:        "edit_file",
		Description: "Replace one exact text fragment in a file; old must occur once.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string","description":"file path"},` +
			`"old":{"type":"string","description":"exact text to replace, must be unique"},` +
			`"new":{"type":"string","description":"replacement text"}},` +
			`"required":["path","old","new"],"additionalProperties":false}`),
	}
}

// ReadOnly is false: edit_file changes files.
func (EditFile) ReadOnly() bool { return false }

// Confirm is true: the user approves each edit.
func (EditFile) Confirm() bool { return true }

// Block tells the model when to use edit_file.
func (EditFile) Block() string {
	return "edit_file: change part of a file. old must match exactly once: include surrounding lines. Whole file: write_file.\n" +
		`Example: {"path":"main.go","old":"fmt.Println(\"hi\")","new":"fmt.Println(\"hello\")"}`
}

// Summary is the path of the call, or "" for bad JSON.
func (EditFile) Summary(args json.RawMessage) string { return pathOf(args) }

// Run replaces the single occurrence of old. A CRLF file is matched on LF text and
// written back with CRLF; a UTF-8 BOM is kept.
func (t EditFile) Run(_ context.Context, env Env, args json.RawMessage) (Result, error) {
	var a struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := DecodeFor(t, args, &a, "path", "old", "new"); err != nil {
		return Result{}, err
	}
	if a.Old == "" {
		return Result{}, errors.New("old is empty; give the exact text to replace")
	}
	if a.Old == a.New {
		return Result{}, errors.New("old and new are the same")
	}
	name, err := Rel(env.Dir, a.Path)
	if err != nil {
		return Result{}, err
	}
	mode, exists, err := statTarget(env.Root, name)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Result{}, fmt.Errorf("%s: no such file", a.Path)
	}
	data, err := env.Root.ReadFile(name)
	if err != nil {
		return Result{}, err
	}
	var bom []byte
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		bom, data = []byte("\xef\xbb\xbf"), data[3:]
	}
	crlf := bytes.Contains(data, []byte("\r\n"))
	text, oldText, newText := string(data), a.Old, a.New
	if crlf {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		oldText = strings.ReplaceAll(oldText, "\r\n", "\n")
		newText = strings.ReplaceAll(newText, "\r\n", "\n")
	}
	switch n := strings.Count(text, oldText); {
	case n == 0:
		return Result{}, errors.New("old text not found")
	case n > 1:
		return Result{}, fmt.Errorf("old text occurs %d times; include more context to make it unique", n)
	}
	text = strings.Replace(text, oldText, newText, 1)
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if err := writeAtomic(env.Root, name, append(bom, text...), mode); err != nil {
		return Result{}, err
	}
	brief := fmt.Sprintf("+%d -%d lines", lineCount(newText), lineCount(oldText))
	return Result{Output: "edited " + a.Path + " (" + brief + ")", Brief: brief}, nil
}
