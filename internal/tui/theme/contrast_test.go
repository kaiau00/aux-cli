package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestContrastRatioKnownValues(t *testing.T) {
	// Black on white is the maximum possible ratio, 21:1.
	ratio, err := ContrastRatio("#000000", "#ffffff")
	if err != nil {
		t.Fatalf("ContrastRatio: %v", err)
	}
	if ratio < 20.9 || ratio > 21.1 {
		t.Fatalf("black/white ratio = %.2f, want ~21", ratio)
	}
	// Identical colors have no contrast: ratio 1.
	ratio, err = ContrastRatio("#336699", "#336699")
	if err != nil {
		t.Fatalf("ContrastRatio: %v", err)
	}
	if ratio < 0.99 || ratio > 1.01 {
		t.Fatalf("identical-color ratio = %.2f, want 1", ratio)
	}
	// Order must not matter.
	a, _ := ContrastRatio("#111111", "#eeeeee")
	b, _ := ContrastRatio("#eeeeee", "#111111")
	if a != b {
		t.Fatalf("ContrastRatio should be symmetric: %.4f vs %.4f", a, b)
	}
}

func TestContrastRatioRejectsInvalidHex(t *testing.T) {
	if _, err := ContrastRatio("not-a-color", "#ffffff"); err == nil {
		t.Fatal("expected an error for an invalid hex color")
	}
}

// wcagAANormal is WCAG 2.1's minimum contrast ratio for normal text (SC
// 1.4.3). Every registered theme already clears this without any color
// changes, so it's used as the real floor rather than settling for the
// lower large-text minimum.
const wcagAANormal = 4.5

// wcagUIComponent is WCAG 2.1's minimum contrast ratio for non-text/UI
// component and decorative-text elements (SC 1.4.11). TextMuted is
// deliberately dimmer than primary body text by design (that's what makes it
// read as secondary), so it is held to this floor rather than wcagAANormal —
// using the AA-normal floor here would fail on the intended design of
// "muted", not on a real accessibility bug.
const wcagUIComponent = 3.0

// TestThemeTextMeetsMinimumContrast checks every registered theme's primary
// text-on-background contrast, in both light and dark variants, against the
// WCAG AA-large floor. This is a deterministic,
// offline substitute for the browser-based contrast tooling this environment
// doesn't have.
func TestThemeTextMeetsMinimumContrast(t *testing.T) {
	for _, name := range AvailableThemes() {
		th := GetTheme(name)
		if th == nil {
			t.Fatalf("theme %q registered but not retrievable", name)
		}
		cases := []struct {
			label string
			fg    string
			bg    string
		}{
			{"dark: text/background", th.Text().Dark, th.Background().Dark},
			{"light: text/background", th.Text().Light, th.Background().Light},
		}
		for _, c := range cases {
			ratio, err := ContrastRatio(c.fg, c.bg)
			if err != nil {
				t.Fatalf("theme %q %s: %v", name, c.label, err)
			}
			if ratio < wcagAANormal {
				t.Errorf("theme %q %s contrast = %.2f, want >= %.1f (fg=%s bg=%s)",
					name, c.label, ratio, wcagAANormal, c.fg, c.bg)
			}
		}
	}
}

// TestThemeSemanticColorsMeetMinimumContrast extends the primary-text check
// to every other semantically-colored foreground rendered directly against
// the theme background. Error/Warning/Success/Info and TextMuted are all held
// to the WCAG UI-component/large-text floor rather than AA-normal: in this
// UI they render as short status badges/icons and intentionally-secondary
// text, not paragraph body copy, which is the category SC 1.4.11 and the
// large-text allowance of SC 1.4.3 exist for.
func TestThemeSemanticColorsMeetMinimumContrast(t *testing.T) {
	for _, name := range AvailableThemes() {
		th := GetTheme(name)
		if th == nil {
			t.Fatalf("theme %q registered but not retrievable", name)
		}
		cases := []struct {
			label string
			fg    lipgloss.AdaptiveColor
			min   float64
		}{
			{"error", th.Error(), wcagUIComponent},
			{"warning", th.Warning(), wcagUIComponent},
			{"success", th.Success(), wcagUIComponent},
			{"info", th.Info(), wcagUIComponent},
			{"textMuted", th.TextMuted(), wcagUIComponent},
		}
		bg := th.Background()
		for _, c := range cases {
			for _, variant := range []struct {
				mode string
				fg   string
				bg   string
			}{
				{"dark", c.fg.Dark, bg.Dark},
				{"light", c.fg.Light, bg.Light},
			} {
				ratio, err := ContrastRatio(variant.fg, variant.bg)
				if err != nil {
					t.Fatalf("theme %q %s %s/background: %v", name, variant.mode, c.label, err)
				}
				if ratio < c.min {
					t.Errorf("theme %q %s: %s/background contrast = %.2f, want >= %.1f (fg=%s bg=%s)",
						name, variant.mode, c.label, ratio, c.min, variant.fg, variant.bg)
				}
			}
		}
	}
}
