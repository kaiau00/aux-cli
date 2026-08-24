package styles

import (
	"os"
	"strings"
)

// SupportsUnicode reports whether the terminal is expected to render
// non-ASCII glyphs reliably. Most terminals handle Unicode fine even with no locale
// configured, so the only cases treated as ASCII-only are an explicit
// override or the classic POSIX "C"/"POSIX" locale — the traditional signal
// for "ASCII only" — rather than defaulting to degraded glyphs whenever
// LANG happens to be unset.
func SupportsUnicode() bool {
	if v := os.Getenv("AUX_ASCII_ICONS"); v == "1" || strings.EqualFold(v, "true") {
		return false
	}
	if v := os.Getenv("AUX_UNICODE_ICONS"); v == "1" || strings.EqualFold(v, "true") {
		return true
	}
	for _, env := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		v := os.Getenv(env)
		if v == "" {
			continue
		}
		upper := strings.ToUpper(v)
		// A locale that names an encoding is trusted over its language part.
		// C.UTF-8 is the default on most Linux distributions, container images,
		// and CI runners, and it supports Unicode fully — an earlier version
		// treated every locale beginning "C." as ASCII-only and so degraded the
		// icons for a large share of Linux users.
		if strings.Contains(upper, "UTF-8") || strings.Contains(upper, "UTF8") {
			return true
		}
		return upper != "C" && upper != "POSIX"
	}
	return true
}

func pickIcon(unicode, ascii string) string {
	if SupportsUnicode() {
		return unicode
	}
	return ascii
}

var (
	// Every glyph here is present in both SF Mono and Menlo -- the default and
	// fallback monospace fonts of macOS Terminal. A glyph missing from the
	// terminal's font is substituted from another one, which need not respect
	// the cell grid, and codepoints with an emoji presentation (U+26A0 warning,
	// U+2139 information) can come back double-width and coloured while the
	// layout still counts them as one column. See TestIconsAreFontSafe.
	AuxIcon = pickIcon("●", "*")

	CheckIcon    = pickIcon("✓", "v")
	ErrorIcon    = pickIcon("✗", "x")
	WarningIcon  = pickIcon("▲", "!")
	InfoIcon     = pickIcon("•", "i")
	HintIcon     = "i"   // already ASCII
	SpinnerIcon  = "..." // already ASCII
	DocumentIcon = pickIcon("□", "#")
	PinIcon      = pickIcon("■", "*")
	// AgentDropIcon marks a context page the agent dropped itself, so the user
	// can tell it apart from one they crossed off and can put it back.
	AgentDropIcon = pickIcon("×", "~")
)
