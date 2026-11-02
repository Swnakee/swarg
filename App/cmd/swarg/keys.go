package main

import (
	"io"
	"maps"
	"slices"

	"swarg/internal/config"
	"swarg/internal/keys"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// /keys (R-106, R-110): the global keys file as ui.KeysAPI (view only)
// ---------------------------------------------------------------------------

// keysView lists the keys the loaded config refers to plus the ones found in the files. The store
// is the user's (<config dir>/keys.env), re-resolved and re-read on every call.
type keysView struct {
	cfg *config.Config
}

var _ ui.KeysAPI = keysView{}

// Keys is one row per name: the config's providers and MCP values, then any other key in the files.
func (v keysView) Keys() []ui.KeyRow {
	store := keys.User()
	users := config.KeyUsers(*v.cfg)
	names := slices.AppendSeq(store.Names(), maps.Keys(users))
	slices.Sort(names)
	uniq := slices.Compact(names)
	states := store.States(uniq)
	rows := make([]ui.KeyRow, len(uniq))
	for i, n := range uniq {
		st := states[i]
		row := ui.KeyRow{Name: n, Users: users[n], State: "missing"}
		switch {
		case st.Env && (st.File || st.Old):
			row.State = "set (env, wins)"
		case st.Env:
			row.State = "set (env)"
		case st.File:
			row.State = "set (" + keys.SrcFile + ")"
		case st.Old:
			row.State = "set (" + keys.SrcOld + ")"
		}
		if st.Value != "" {
			row.Masked = keys.MaskKey(st.Value)
		}
		rows[i] = row
	}
	return rows
}

// File is the keys file path shown in the menu.
func (v keysView) File() string { return keys.User().File }

// Warnings are the current problems of the keys file (never a key).
func (v keysView) Warnings() []string { return keys.User().Warnings() }

// Init writes the template with the key names the config uses; it never overwrites a file.
func (v keysView) Init() (string, error) {
	store := keys.User()
	var names []string
	for n := range config.KeyUsers(*v.cfg) {
		names = append(names, n)
	}
	return store.File, store.Init(names)
}

// keysNotices prints the start notices about the keys: the problems of the keys file (not private,
// bad line numbers; never a key) and, when old key files exist, the hint to move them (R-106).
func keysNotices(w io.Writer) {
	store := keys.User()
	warn(w, store.Warnings()...)
	warn(w, store.OldHint())
}
