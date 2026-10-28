package ui

import "testing"

func TestClean_StripsControlKeepsNewlineAndTab(t *testing.T) {
	in := "a\x1b[31mred\x1b[0m\tb\nc\x07\x7f\u009bd\r"
	if got, want := Clean(in), "a[31mred[0m\tb\ncd"; got != want {
		t.Errorf("Clean = %q, want %q", got, want)
	}
	if got := Clean("Привет 🙂"); got != "Привет 🙂" {
		t.Errorf("Clean changed plain text: %q", got)
	}
}
