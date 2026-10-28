package ui

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestFitProfile_DropsBackgroundsBelow256Colours(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"true-colour bg with fg", "\x1b[38;2;1;2;3;48;2;4;5;6mX\x1b[m", "X\x1b[m"},
		{"bg only, bold kept", "\x1b[1;48;5;23mX", "\x1b[1mX"},
		{"16-colour bg and fg", "\x1b[44;96mX", "X"},
		{"bright bg", "\x1b[0;104mX", "\x1b[0mX"},
		{"fg alone untouched", "\x1b[38;2;95;215;175mX", "\x1b[38;2;95;215;175mX"},
		{"16-colour fg alone untouched", "\x1b[32mX", "\x1b[32mX"},
		{"reverse untouched", "\x1b[7mX\x1b[27m", "\x1b[7mX\x1b[27m"},
		{"plain text", "Привет", "Привет"},
	} {
		if got := fitProfile(tc.in, colorprofile.ANSI); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFitProfile_KeepsColoursAt256AndUnknown(t *testing.T) {
	in := "\x1b[38;2;1;2;3;48;2;4;5;6mX\x1b[m"
	for _, p := range []colorprofile.Profile{colorprofile.Unknown, colorprofile.ANSI256, colorprofile.TrueColor} {
		if got := fitProfile(in, p); got != in {
			t.Errorf("%v: %q changed to %q", p, in, got)
		}
	}
	if got := fitProfile(in, colorprofile.ASCII); got != "X\x1b[m" {
		t.Errorf("ASCII: %q", got)
	}
}
