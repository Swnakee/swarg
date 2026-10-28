package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// ---------------------------------------------------------------------------
// Colour profile fitting
//
// Bubble Tea downsamples every colour to the terminal's profile. In 16 colours a background
// becomes a theme colour the terminal paints as it likes (Ghostty: light cyan, light blue),
// and the text on it may vanish. Below 256 colours the view therefore paints no
// backgrounds: a sequence that sets one loses it together with its paired foreground, so the
// text falls back to the terminal's own colours, which are always readable.
// ---------------------------------------------------------------------------

var sgrRe = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// fitProfile adapts a rendered view to the colour profile p. Unknown (no report yet) and
// 256 or more colours keep the view as it is.
func fitProfile(s string, p colorprofile.Profile) string {
	if p == colorprofile.Unknown || p >= colorprofile.ANSI256 || !strings.Contains(s, "\x1b[") {
		return s
	}
	return sgrRe.ReplaceAllStringFunc(s, dropBackground)
}

// dropBackground removes the background and foreground parameters from one SGR sequence
// when it sets a background; other sequences pass unchanged.
func dropBackground(seq string) string {
	params := strings.Split(seq[2:len(seq)-1], ";")
	hasBg := false
	for i := 0; i < len(params); i++ {
		switch p := params[i]; {
		case p == "48":
			hasBg = true
			i += skipColour(params, i)
		case p == "38":
			i += skipColour(params, i)
		case isBasicBg(p):
			hasBg = true
		}
	}
	if !hasBg {
		return seq
	}
	var keep []string
	for i := 0; i < len(params); i++ {
		switch p := params[i]; {
		case p == "38" || p == "48":
			i += skipColour(params, i)
		case isBasicBg(p) || isBasicFg(p):
		default:
			keep = append(keep, p)
		}
	}
	if len(keep) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(keep, ";") + "m"
}

// skipColour returns how many parameters after params[i] (38 or 48) belong to the colour.
func skipColour(params []string, i int) int {
	if i+1 >= len(params) {
		return 0
	}
	switch params[i+1] {
	case "5":
		return min(2, len(params)-1-i)
	case "2":
		return min(4, len(params)-1-i)
	}
	return 0
}

func isBasicBg(p string) bool { return inRange(p, 40, 47) || inRange(p, 100, 107) }
func isBasicFg(p string) bool { return inRange(p, 30, 37) || inRange(p, 90, 97) }

func inRange(p string, lo, hi int) bool {
	n := 0
	for _, r := range p {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
	}
	return p != "" && n >= lo && n <= hi
}
