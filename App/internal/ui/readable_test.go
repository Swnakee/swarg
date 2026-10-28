package ui

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Readability: every kind of content stays readable in every colour profile.
//
// Bubble Tea downsamples the view to the terminal's colour profile. In 16 colours the
// terminal theme decides what a background looks like (Ghostty paints cyan light), so a
// background there can hide its text. In 256 and true colour the colours are ours: a cell on
// a background must carry its own foreground (a light-theme default would vanish on a dark
// background) with enough contrast.
// ---------------------------------------------------------------------------

// darkBg is a typical dark terminal background, used to judge foreground-only cells.
const darkBg = 0x202030

const (
	minContrastOnBg = 4.5 // WCAG AA for text on a background we paint
	minContrastFg   = 3.0 // a coloured foreground on the terminal's own dark background
)

type rgb struct{ r, g, b float64 }

type cell struct {
	ch             rune
	fg, bg         *rgb
	fgSet, bgSet   bool // any colour parameter, also for 16-colour codes
	fgBlack        bool // 16-colour black or bright black: dark on a dark terminal
	reverse, faint bool
}

var (
	oscSeq = regexp.MustCompile("\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)")
	sgrSeq = regexp.MustCompile("\x1b\\[([0-9;:]*)m")
	csiSeq = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-ln-z]")
)

// ---------------------------------------------------------------------------
// SGR parsing
// ---------------------------------------------------------------------------

// xterm256 returns the RGB of a 256-colour index (16..255; 0..15 are theme colours).
func xterm256(n int) *rgb {
	switch {
	case n < 16:
		return nil
	case n < 232:
		n -= 16
		lv := func(v int) float64 {
			if v == 0 {
				return 0
			}
			return float64(55 + v*40)
		}
		return &rgb{lv(n / 36), lv(n / 6 % 6), lv(n % 6)}
	default:
		v := float64(8 + (n-232)*10)
		return &rgb{v, v, v}
	}
}

// applySGR updates c with the parameters of one SGR sequence.
func applySGR(c *cell, params string) {
	if params == "" {
		*c = cell{}
		return
	}
	p := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	num := func(i int) int { v, _ := strconv.Atoi(p[i]); return v }
	for i := 0; i < len(p); i++ {
		n := num(i)
		switch {
		case n == 0:
			*c = cell{}
		case n == 2:
			c.faint = true
		case n == 7:
			c.reverse = true
		case n == 27:
			c.reverse = false
		case n == 22:
			c.faint = false
		case n == 39:
			c.fg, c.fgSet, c.fgBlack = nil, false, false
		case n == 49:
			c.bg, c.bgSet = nil, false
		case n == 38 || n == 48:
			var col *rgb
			if i+1 < len(p) && num(i+1) == 2 && i+4 < len(p) {
				col = &rgb{float64(num(i + 2)), float64(num(i + 3)), float64(num(i + 4))}
				i += 4
			} else if i+2 < len(p) && num(i+1) == 5 {
				col = xterm256(num(i + 2))
				i += 2
			}
			if n == 38 {
				c.fg, c.fgSet, c.fgBlack = col, true, false
			} else {
				c.bg, c.bgSet = col, true
			}
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			c.fg, c.fgSet, c.fgBlack = nil, true, n == 30
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			c.bg, c.bgSet = nil, true
		}
	}
}

// cells returns every printed non-blank character of s with its attributes.
func cells(s string) []cell {
	s = csiSeq.ReplaceAllString(oscSeq.ReplaceAllString(s, ""), "")
	var out []cell
	var cur cell
	last := 0
	flush := func(text string) {
		for _, r := range text {
			if r == ' ' || r == '\n' || r == '\t' || r == ' ' {
				continue
			}
			c := cur
			c.ch = r
			out = append(out, c)
		}
	}
	for _, m := range sgrSeq.FindAllStringSubmatchIndex(s, -1) {
		flush(s[last:m[0]])
		applySGR(&cur, s[m[2]:m[3]])
		last = m[1]
	}
	flush(s[last:])
	return out
}

// ---------------------------------------------------------------------------
// Contrast (WCAG 2)
// ---------------------------------------------------------------------------

func luminance(c rgb) float64 {
	ch := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.r) + 0.7152*ch(c.g) + 0.0722*ch(c.b)
}

func contrast(a, b rgb) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func hexRGB(v int) rgb {
	return rgb{float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff)}
}

// ---------------------------------------------------------------------------
// Checks
// ---------------------------------------------------------------------------

// downsample is what Bubble Tea does to the view before it reaches the terminal.
func downsample(s string, p colorprofile.Profile) string {
	var b bytes.Buffer
	w := &colorprofile.Writer{Forward: &b, Profile: p}
	_, _ = w.Write([]byte(s))
	return b.String()
}

// unreadable lists the cells of a downsampled view that a user may not be able to read.
func unreadable(view string, p colorprofile.Profile) []string {
	var bad []string
	seen := map[string]int{}
	add := func(c cell, why string) {
		if seen[why]++; seen[why] <= 3 { // a few samples per reason, every reason shown
			bad = append(bad, fmt.Sprintf("%q: %s", string(c.ch), why))
		}
	}
	for _, c := range cells(view) {
		if c.reverse && !c.fgSet && !c.bgSet {
			continue // plain reverse video: the terminal swaps its own colours
		}
		fg, bg, fgSet, bgSet := c.fg, c.bg, c.fgSet, c.bgSet
		if c.reverse {
			fg, bg, fgSet, bgSet = bg, fg, bgSet, fgSet
		}
		switch {
		case !bgSet && c.fgBlack && !c.reverse:
			add(c, "black text in a 16-colour profile")
		case !bgSet:
			if p >= colorprofile.ANSI256 && fg != nil && !c.faint {
				if r := contrast(*fg, hexRGB(darkBg)); r < minContrastFg {
					add(c, fmt.Sprintf("foreground contrast %.1f on a dark terminal", r))
				}
			}
		case p < colorprofile.ANSI256:
			add(c, "background in a 16-colour profile (the terminal theme decides its look)")
		case !fgSet:
			add(c, "background without its own foreground")
		case fg == nil || bg == nil:
			add(c, "theme colour (0..15) mixed with a background")
		default:
			if r := contrast(*fg, *bg); r < minContrastOnBg {
				add(c, fmt.Sprintf("contrast %.1f on its background", r))
			}
		}
	}
	return bad
}

var profiles = []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII}

// richMarkdown has every markdown element the theme styles.
const richMarkdown = "# Heading\n\n## Sub heading\n\nText with **bold**, *italic*, `inline code` and a " +
	"[link](https://example.com).\n\n- item `sudo`\n- item two\n\n1. first\n2. second\n\n> a quote\n\n" +
	"```go\npackage main\n\nfunc main() { println(\"hi\") } // comment\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n---\n\nend\n"

// richRig is a screen with every kind of content: user messages (short, long, Cyrillic),
// markdown, tool blocks open and closed, an error, a queued message notice.
func richRig(t *testing.T, p colorprofile.Profile) *rig {
	r := newRig(t)
	r.send(tea.ColorProfileMsg{Profile: p})
	r.events(started("Привет"), delta("Привет! Как я могу помочь?"), ended(chat.FinishStop, nil))
	r.events(started("так ты кто, и вот очень длинное сообщение пользователя, которое точно не поместится в одну строку экрана"),
		delta(richMarkdown),
		toolStart("a", "read_file", "main.go"),
		toolEnd("a", "read_file", "main.go", "3 lines", "alpha\nbeta\ngamma\n", nil),
		toolStart("b", "shell", "go build"),
		toolEnd("b", "shell", "go build", "", "", errors.New("exit 1: undefined: foo")),
		delta("done"), ended(chat.FinishStop, nil))
	r.open(1)
	r.open(2)
	r.events(agent.Event{Kind: agent.EvTurnEnded, Err: errors.New("server said no")})
	return r
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestReadable_EveryContentInEveryProfile(t *testing.T) {
	for _, p := range profiles {
		t.Run(p.String(), func(t *testing.T) {
			r := richRig(t, p)
			r.send(tea.WindowSizeMsg{Width: 80, Height: 200})
			view := downsample(r.m.View().Content, p)
			if bad := unreadable(view, p); len(bad) > 0 {
				t.Errorf("unreadable cells:\n  %s", strings.Join(bad, "\n  "))
			}
			for _, want := range []string{"Привет", "так ты кто", "inline code", "package main", "read_file"} {
				if !strings.Contains(r.view(), want) {
					t.Errorf("view lacks %q", want)
				}
			}
		})
	}
}

func TestReadable_SelectionInEveryProfile(t *testing.T) {
	for _, p := range profiles {
		t.Run(p.String(), func(t *testing.T) {
			r := richRig(t, p)
			r.send(tea.WindowSizeMsg{Width: 80, Height: 200})
			r.m.sel = selection{active: true, a: selPoint{0, 0, 0}, b: selPoint{len(r.m.cv.items) - 1, 1000, 79}}
			view := downsample(r.m.View().Content, p)
			if bad := unreadable(view, p); len(bad) > 0 {
				t.Errorf("unreadable selected cells:\n  %s", strings.Join(bad, "\n  "))
			}
		})
	}
}

func TestReadable_SessionPickerInEveryProfile(t *testing.T) {
	rows := []PickRow{
		{ID: "a", Title: "первая сессия", Model: "qwen", Time: time.Unix(1000, 0), Messages: 3},
		{ID: "b", Title: "second", Model: "qwen", Time: time.Unix(2000, 0), Messages: 5},
	}
	for _, p := range profiles {
		t.Run(p.String(), func(t *testing.T) {
			pk := newPicker(rows)
			pk.Update(tea.ColorProfileMsg{Profile: p})
			view := downsample(pk.View().Content, p)
			if bad := unreadable(view, p); len(bad) > 0 {
				t.Errorf("unreadable picker cells:\n  %s", strings.Join(bad, "\n  "))
			}
			for _, y := range []int{1, 2} { // the selected row and a plain row, hovered
				pk.Update(tea.MouseMotionMsg{X: 3, Y: y, Button: tea.MouseNone})
				view := downsample(pk.View().Content, p)
				if bad := unreadable(view, p); len(bad) > 0 {
					t.Errorf("unreadable hovered picker row %d:\n  %s", y, strings.Join(bad, "\n  "))
				}
			}
		})
	}
}

// Command suggestions and the highlighted input (R-35) stay readable in every profile.
func TestReadable_SuggestionsInEveryProfile(t *testing.T) {
	for _, p := range profiles {
		t.Run(p.String(), func(t *testing.T) {
			r := richRig(t, p)
			r.send(tea.WindowSizeMsg{Width: 80, Height: 60})
			r.typeIn("/re")
			if !r.m.suggesting() {
				t.Fatal("no suggestions")
			}
			view := downsample(r.m.View().Content, p)
			if bad := unreadable(view, p); len(bad) > 0 {
				t.Errorf("unreadable cells with suggestions:\n  %s", strings.Join(bad, "\n  "))
			}
			y := rowOf(r, "/rename <name>")
			r.hoverTo(3, y)
			if bad := unreadable(downsample(r.m.View().Content, p), p); len(bad) > 0 {
				t.Errorf("unreadable cells with a hovered suggestion:\n  %s", strings.Join(bad, "\n  "))
			}
		})
	}
}

// Hover (R-36): a hovered tool head stays readable, open and closed, in every profile.
func TestReadable_HoverInEveryProfile(t *testing.T) {
	for _, p := range profiles {
		t.Run(p.String(), func(t *testing.T) {
			r := richRig(t, p)
			r.send(tea.WindowSizeMsg{Width: 80, Height: 200})
			hovered := 0
			for y, row := range r.rows() {
				if !strings.Contains(row, glyphCall+" ") {
					continue
				}
				r.hoverTo(3, y)
				raw := r.m.View().Content
				if !strings.Contains(strings.Split(raw, "\n")[y], underlineOn) {
					t.Errorf("row %d (%q) not underlined on hover", y, row)
				}
				if bad := unreadable(downsample(raw, p), p); len(bad) > 0 {
					t.Errorf("unreadable cells with the hover on row %d:\n  %s", y, strings.Join(bad, "\n  "))
				}
				hovered++
			}
			if hovered < 2 {
				t.Errorf("only %d tool heads hovered", hovered)
			}
		})
	}
}

// The checker itself: it must flag the owner's case (a 16-colour background under default
// text) and pass plain text.
func TestReadable_CheckerFlagsBackgroundIn16Colours(t *testing.T) {
	owner := downsample("\x1b[48;2;38;50;63m▸ Привет\x1b[m", colorprofile.ANSI)
	if len(unreadable(owner, colorprofile.ANSI)) == 0 {
		t.Error("a 16-colour background was not flagged")
	}
	noFg := "\x1b[48;2;38;50;63mtext\x1b[m"
	if len(unreadable(noFg, colorprofile.TrueColor)) == 0 {
		t.Error("a background without a foreground was not flagged")
	}
	low := "\x1b[38;2;40;40;40;48;2;30;30;30mtext\x1b[m"
	if len(unreadable(low, colorprofile.TrueColor)) == 0 {
		t.Error("low contrast was not flagged")
	}
	if bad := unreadable("plain \x1b[1mbold\x1b[m \x1b[7msel\x1b[27m", colorprofile.ANSI); len(bad) > 0 {
		t.Errorf("plain text flagged: %v", bad)
	}
}
