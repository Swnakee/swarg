package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"swarg/internal/session"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Picking a session
// ---------------------------------------------------------------------------

// pickSession returns the session of infos (newest first) whose id starts with id; an empty
// id picks the newest readable one. Broken files are never picked. An unknown or ambiguous
// id is an error listing the candidates. With none true an empty id and no sessions return
// (zero, nil) instead of an error.
func pickSession(infos []session.Info, id string, none bool) (session.Info, error) {
	var ok, match []session.Info
	for _, in := range infos {
		if in.Err == "" {
			ok = append(ok, in)
		}
		// a broken file still matches by the id in its name, so resuming it reports the real error
		if id != "" && strings.HasPrefix(fileID(in), id) {
			match = append(match, in)
		}
	}
	switch {
	case id == "" && len(ok) > 0:
		return ok[0], nil
	case id == "" && none:
		return session.Info{}, nil
	case id == "":
		return session.Info{}, fmt.Errorf("no saved sessions in this project")
	case len(match) == 1:
		return match[0], nil
	case len(match) > 1:
		return session.Info{}, fmt.Errorf("session id %q is ambiguous:\n%s", id, sessionTable(match))
	case len(ok) == 0:
		return session.Info{}, fmt.Errorf("no session %q: no saved sessions in this project", id)
	}
	return session.Info{}, fmt.Errorf("no session %q; sessions of this project:\n%s", id, sessionTable(ok))
}

// fileID is the session id of in; for a broken file (no id) the one in its file name
// YYYYMMDD-HHMMSS-<id>.jsonl.
func fileID(in session.Info) string {
	if in.ID != "" {
		return in.ID
	}
	stem := strings.TrimSuffix(filepath.Base(in.Path), ".jsonl")
	return stem[strings.LastIndex(stem, "-")+1:]
}

// sessionTable renders `id  started  model  messages  title`, one line per session.
func sessionTable(infos []session.Info) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, in := range infos {
		if in.Err != "" {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", in.ID, in.Started.Format("2006-01-02 15:04"), in.Model, in.Messages, ui.Clean(in.Title))
	}
	tw.Flush()
	return strings.TrimRight(b.String(), "\n")
}

// printSessions implements --sessions.
func printSessions(infos []session.Info, out io.Writer) {
	if t := sessionTable(infos); t != "" {
		fmt.Fprintln(out, t)
		return
	}
	fmt.Fprintln(out, "no saved sessions in this project")
}

// choosePick implements -r without an id. It returns the chosen session id, or done with the
// exit code when the run ends here: empty list (0), cancelled (0), no terminal (2: the list
// goes to out and a hint to errOut), picker failure (1).
func choosePick(infos []session.Info, tty bool, pick func([]ui.PickRow, func(string) error) (string, bool, error), del func(string) error, out, errOut io.Writer) (id string, code int, done bool) {
	if len(infos) == 0 {
		fmt.Fprintln(out, "no saved sessions in this folder")
		return "", 0, true
	}
	if !tty {
		printSessions(infos, out)
		fmt.Fprintln(errOut, "swarg: no terminal to choose in: use swarg -r <id>")
		return "", 2, true
	}
	rows := make([]ui.PickRow, len(infos))
	for i, in := range infos {
		rows[i] = ui.PickRow{ID: fileID(in), Title: in.Title, Model: in.Model, Time: in.Started, Messages: in.Messages, Err: in.Err}
	}
	id, ok, err := pick(rows, del)
	switch {
	case err != nil:
		fmt.Fprintf(errOut, "swarg: %v\n", err)
		return "", 1, true
	case !ok:
		return "", 0, true
	}
	return id, 0, false
}

// ---------------------------------------------------------------------------
// Model choice at start (R-30)
// ---------------------------------------------------------------------------

// startModel is the model a run uses: the --model flag, else the resumed session's last
// choice, else the configured one (config file, SWARG_MODEL). A resumed session therefore
// continues with its own model even inside the sandbox, which sets SWARG_MODEL.
func startModel(flag, resumed, configured string) string {
	switch {
	case flag != "":
		return flag
	case resumed != "":
		return resumed
	}
	return configured
}
