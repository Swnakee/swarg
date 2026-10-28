package session

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// Project directory
// ---------------------------------------------------------------------------

// Dir returns the SessionFile directory of the project at cwd:
// base/sessions/<slug>-<8 hex of sha256 of the normalized path>. The path is
// normalized with EvalSymlinks (when it exists) and Clean, and lowercased on
// the case-insensitive platforms, so one project has one directory.
func Dir(base, cwd string) string {
	p := cwd
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	sum := sha256.Sum256([]byte(p))
	return filepath.Join(base, "sessions", slug(p)+"-"+hex.EncodeToString(sum[:4]))
}

// slug is the last path element reduced to [a-z0-9._-], at most 40 bytes.
func slug(p string) string {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
	last := "root"
	if len(parts) > 0 {
		last = parts[len(parts)-1]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(last) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), ".")
	if s == "" {
		s = "root"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// TracePath returns where the Trace of the SessionFile at sessionPath lives:
// <dir>/traces/<file name without .jsonl>.trace.jsonl.
func TracePath(sessionPath string) string {
	stem := strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl")
	return filepath.Join(filepath.Dir(sessionPath), "traces", stem+".trace.jsonl")
}
