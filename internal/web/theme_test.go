package web

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// themeTokens reads the custom properties of the first rule whose
// selector is sel.
func themeTokens(t *testing.T, css, sel string) map[string]string {
	t.Helper()
	i := strings.Index(css, sel+"{")
	if i < 0 {
		t.Fatalf("no %s rule", sel)
	}
	body := css[i+len(sel)+1:]
	body = body[:strings.Index(body, "}")]
	out := map[string]string{}
	for _, decl := range strings.Split(body, ";") {
		if k, v, ok := strings.Cut(decl, ":"); ok && strings.HasPrefix(k, "--") {
			out[k] = v
		}
	}
	return out
}

func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	if !regexp.MustCompile(`^#[0-9a-f]{6}$`).MatchString(hex) {
		if hex == "#fff" {
			hex = "#ffffff"
		} else {
			t.Fatalf("colour %q is not #rrggbb", hex)
		}
	}
	var l [3]float64
	for i := range l {
		n, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(n) / 255
		if c <= 0.04045 {
			l[i] = c / 12.92
		} else {
			l[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
}

// TKT-01M3JV462: both themes define the same tokens, the dark theme is
// the same whether chosen or followed from the system, and every text
// colour meets WCAG AA (4.5:1) on the backgrounds it is drawn on.
func TestThemesMeetContrast(t *testing.T) {
	raw, err := files.ReadFile("assets/lake.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	light := themeTokens(t, css, ":root")
	dark := themeTokens(t, css, ":root[data-theme=dark]")
	system := themeTokens(t, css, ":root:not([data-theme=light])")
	if len(light) != len(dark) {
		t.Fatalf("light has %d tokens, dark %d", len(light), len(dark))
	}
	for k, v := range dark {
		if _, ok := light[k]; !ok {
			t.Errorf("%s is only in the dark theme", k)
		}
		if system[k] != v {
			t.Errorf("%s: chosen dark %s, system dark %s", k, v, system[k])
		}
	}
	for _, used := range regexp.MustCompile(`var\((--[a-z0-9-]+)\)`).FindAllStringSubmatch(css, -1) {
		if _, ok := light[used[1]]; !ok {
			t.Errorf("%s is used but not defined", used[1])
		}
	}
	pairs := [][2]string{
		{"--ink", "--paper"}, {"--ink", "--surface"}, {"--ink", "--surface-2"}, {"--ink", "--highlight"}, {"--ink", "--mark"}, {"--ink", "--soft"},
		{"--muted", "--paper"}, {"--muted", "--surface"}, {"--muted", "--surface-2"},
		{"--accent", "--paper"}, {"--accent", "--surface"}, {"--accent", "--surface-2"},
		{"--on-accent", "--accent"}, {"--on-accent", "--accent-strong"},
		{"--chip-ink", "--chip"}, {"--ok", "--ok-bg"}, {"--warn", "--warn-bg"}, {"--bad", "--bad-bg"}, {"--bad", "--bad-soft"}, {"--neutral", "--neutral-bg"},
	}
	for name, theme := range map[string]map[string]string{"light": light, "dark": dark} {
		for _, p := range pairs {
			a, b := luminance(t, theme[p[0]]), luminance(t, theme[p[1]])
			ratio := (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05)
			if ratio < 4.5 {
				t.Errorf("%s: %s on %s is %.2f:1", name, p[0], p[1], ratio)
			}
		}
	}
}

// TKT-01M3JV462: the remembered theme is applied by a blocking script
// served from the lake, ahead of the stylesheet so the page never paints
// in the other theme, and the toggle stays hidden without scripts.
func TestPagesLoadThemeBeforePaint(t *testing.T) {
	_, idp, h, _ := fixture(t)
	cookie, _ := signIn(t, idp, h)
	body := get(h, "/", cookie).Body.String()
	script := strings.Index(body, `<script src="/assets/theme.js"></script>`)
	sheet := strings.Index(body, `<link rel="stylesheet" href="/assets/lake.css">`)
	if script < 0 || sheet < 0 || script > sheet {
		t.Fatalf("theme.js at %d, stylesheet at %d", script, sheet)
	}
	if !strings.Contains(body, `<button id="theme" class="quiet" type="button" hidden>`) {
		t.Fatal("theme toggle is missing or shown without scripts")
	}
	if w := get(h, "/assets/theme.js", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "lampi-theme") {
		t.Fatalf("theme.js: %d", w.Code)
	}
}
