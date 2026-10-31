package agent

import (
	"runtime"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Path lock: two conversations never write the same file at the same time (R-124)
// ---------------------------------------------------------------------------

// pathLocks maps a file path to the conversation that is writing it right now.
type pathLocks struct {
	mu    sync.Mutex
	owner map[string]string
}

// pathKey is the comparison form of a path: slash form, and lower case where the file system
// ignores case (macOS, Windows), so "A.go" and "a.go" are one file.
func pathKey(p string) string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// lock takes the path for who; when another holds it, it returns that holder and false.
func (l *pathLocks) lock(path, who string) (holder string, ok bool) {
	k := pathKey(path)
	l.mu.Lock()
	defer l.mu.Unlock()
	if h, busy := l.owner[k]; busy {
		return h, false
	}
	if l.owner == nil {
		l.owner = map[string]string{}
	}
	l.owner[k] = who
	return who, true
}

// unlock frees the path.
func (l *pathLocks) unlock(path string) {
	l.mu.Lock()
	delete(l.owner, pathKey(path))
	l.mu.Unlock()
}
