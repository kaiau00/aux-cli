package styles

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fontSafeGlyphs are the non-ASCII characters the TUI is allowed to render.
// Every one is present in both SF Mono and Menlo, the default and fallback
// monospace fonts of macOS Terminal.
//
// This is not fussiness about fonts. A glyph the terminal's font lacks is
// substituted from another font, which need not honour the cell grid, and a
// codepoint with an emoji presentation comes back double-width and coloured
// while lipgloss still counts it as a single column -- so every line
// containing one is a column out, and the layout looks broken. The TUI shipped
// with twelve such glyphs, including its own "⌬" logo, which is in neither
// font.
//
// Adding a glyph here means checking it against both fonts first.
const fontSafeGlyphs = "·»½×—•…←↑→↓≈─│┃└┼▀░■□▲▶▸○●✓✗"

var goStringLiteral = regexp.MustCompile(`"([^"\\\n]*)"`)

func TestIconsAreFontSafe(t *testing.T) {
	allowed := make(map[rune]bool, len([]rune(fontSafeGlyphs)))
	for _, r := range fontSafeGlyphs {
		allowed[r] = true
	}

	// Walk the whole TUI tree rather than this package alone: the state glyphs
	// live in the components that draw them, which is where the unsafe ones
	// were.
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range goStringLiteral.FindAllStringSubmatch(string(src), -1) {
			for _, r := range match[1] {
				if r > 127 && !allowed[r] {
					t.Errorf("%s: string %q contains %q (U+%04X), which is not in the font-safe set",
						path, match[1], string(r), r)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// The ASCII fallback exists for terminals that cannot render Unicode at all.
// Every icon must actually have one, and it must be ASCII -- an earlier version
// left InfoIcon empty, which rendered as nothing at all.
func TestEveryIconHasAnASCIIFallback(t *testing.T) {
	t.Setenv("AUX_UNICODE_ICONS", "")
	t.Setenv("AUX_ASCII_ICONS", "1")

	for name, icon := range map[string]string{
		"AuxIcon":       pickIcon("●", "*"),
		"CheckIcon":     pickIcon("✓", "v"),
		"ErrorIcon":     pickIcon("✗", "x"),
		"WarningIcon":   pickIcon("▲", "!"),
		"InfoIcon":      pickIcon("•", "i"),
		"DocumentIcon":  pickIcon("□", "#"),
		"PinIcon":       pickIcon("■", "*"),
		"AgentDropIcon": pickIcon("×", "~"),
	} {
		if icon == "" {
			t.Errorf("%s has an empty ASCII fallback; it would render as nothing", name)
		}
		for _, r := range icon {
			if r > 127 {
				t.Errorf("%s falls back to %q, which is not ASCII", name, icon)
			}
		}
	}
}
