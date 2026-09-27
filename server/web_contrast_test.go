package server

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// tokenBlock returns the hex custom properties declared in the first block
// of 00-core.css opened by head (e.g. ":root {").
func tokenBlock(t *testing.T, css, head string) map[string]string {
	t.Helper()
	i := strings.Index(css, head)
	if i < 0 {
		t.Fatalf("00-core.css has no %q block", head)
	}
	body := css[i+len(head):]
	body = body[:strings.Index(body, "\n}")]
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(--[a-z0-9-]+):\s*(#[0-9a-fA-F]{6}|var\(--[a-z0-9-]+\))`).FindAllStringSubmatch(body, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// luminance is the WCAG 2 relative luminance of a #rrggbb colour.
func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	var c [3]float64
	for i := range c {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("bad colour %q", hex)
		}
		s := float64(v) / 255
		if s <= 0.03928 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func contrast(t *testing.T, a, b string) float64 {
	x, y := luminance(t, a), luminance(t, b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}

// The text ramp's lightest steps (--base-40 = --muted "labels, meta" and
// --base-50 "secondary text") clear WCAG AA 4.5:1 on every surface muted
// text is drawn on. Light: the whole surface ramp down to the selected row
// (base-20). JARVIS: the surfaces measured under muted text on 2026-09-27 —
// the app ground (base-00), primary surface (base-05) and rail (base-10).
// --base-40 was #a3a3a3, 2.13–2.52:1; this is arithmetic, not a screenshot.
func TestWebTextTokensClearAA(t *testing.T) {
	raw, err := os.ReadFile("web/css/00-core.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	light := tokenBlock(t, css, "\n:root {")
	if light["--muted"] != "var(--base-40)" {
		t.Fatalf("--muted is %q, not var(--base-40): point this test at its value", light["--muted"])
	}
	jarvis := tokenBlock(t, css, `:root[data-theme="jarvis"] {`)
	text := []string{"--base-40", "--base-50", "--base-60", "--base-70", "--base-80", "--base-100"}
	for _, theme := range []struct {
		name     string
		tokens   map[string]string
		surfaces []string
	}{
		{"light", light, []string{"--base-00", "--base-05", "--base-08", "--base-10", "--base-15", "--base-20"}},
		{"jarvis", jarvis, []string{"--base-00", "--base-05", "--base-10"}},
	} {
		for _, fg := range text {
			for _, bg := range theme.surfaces {
				f, b := theme.tokens[fg], theme.tokens[bg]
				if f == "" || b == "" {
					t.Fatalf("%s: %s or %s is not a hex token", theme.name, fg, bg)
				}
				if r := contrast(t, f, b); r < 4.5 {
					t.Errorf("%s: %s %s on %s %s is %.2f:1, below AA 4.5:1", theme.name, fg, f, bg, b, r)
				}
			}
		}
	}
	// the light ramp keeps its order: muted is never darker than secondary
	if l40, l50 := luminance(t, light["--base-40"]), luminance(t, light["--base-50"]); l40 < l50 {
		t.Errorf("light --base-40 %s is darker than --base-50 %s", light["--base-40"], light["--base-50"])
	}
}
