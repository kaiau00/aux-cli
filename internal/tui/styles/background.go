package styles

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// resolveAdaptiveColor picks the dark/light hex string for color using the
// renderer's current dark/light setting — the same explicit pin the app sets
// at startup (see app.initTheme), not a fresh terminal query.
func resolveAdaptiveColor(color lipgloss.AdaptiveColor) string {
	if lipgloss.HasDarkBackground() {
		return color.Dark
	}
	return color.Light
}

// ForceReplaceBackgroundWithLipgloss replaces any ANSI background color codes
// in `input` with newBgColor, downsampled to whatever color profile the
// terminal actually supports (TrueColor/ANSI256/ANSI/Ascii) via
// lipgloss.ColorProfile(), instead of always emitting a raw 24-bit sequence.
//
// This matters concretely: a terminal with incomplete or no 24-bit support —
// notably Apple's Terminal.app, which many termenv/lipgloss-based tools flag
// as ANSI256 at best — can silently drop or misrender an unsupported 24-bit
// background code. The background then falls through to whatever was already
// active nearby (e.g. a correctly-downsampled accent color from an adjacent
// styled run), while the foreground text (rendered through glamour's own
// profile-aware path) renders normally — producing text that's present and
// selectable but visually unreadable against the wrong background.
func ForceReplaceBackgroundWithLipgloss(input string, newBgColor lipgloss.AdaptiveColor) string {
	hex := resolveAdaptiveColor(newBgColor)
	return rewriteBackground(input, lipgloss.ColorProfile().Color(hex).Sequence(true))
}

// StripBackgrounds removes every background colour code from input, leaving
// whatever the terminal already paints showing through.
//
// This is the canvas case. ForceReplaceBackgroundWithLipgloss exists for the
// opposite one -- a dialog or other surface that has to be opaque, because an
// overlay you can see the transcript through is unreadable.
//
// It solves the Terminal.app problem in that function's comment more directly:
// with no background code emitted at all, there is nothing for a terminal with
// incomplete 24-bit support to drop, and so nothing that can fall through to an
// adjacent accent.
func StripBackgrounds(input string) string {
	return rewriteBackground(input, "")
}

// rewriteBackground drops every background code in input and appends newBg,
// which may be empty to mean "leave the terminal's own background".
func rewriteBackground(input, newBg string) string {
	return ansiEscape.ReplaceAllStringFunc(input, func(seq string) string {
		const (
			escPrefixLen = 2 // "\x1b["
			escSuffixLen = 1 // "m"
		)

		raw := seq
		start := escPrefixLen
		end := len(raw) - escSuffixLen

		var sb strings.Builder
		// reserve enough space: original content minus bg codes + our newBg
		sb.Grow((end - start) + len(newBg) + 2)

		// scan from start..end, token by token
		for i := start; i < end; {
			// find the next ';' or end
			j := i
			for j < end && raw[j] != ';' {
				j++
			}
			token := raw[i:j]

			// fast‑path: skip "48;5;N" or "48;2;R;G;B"
			if len(token) == 2 && token[0] == '4' && token[1] == '8' {
				k := j + 1
				if k < end {
					// find next token
					l := k
					for l < end && raw[l] != ';' {
						l++
					}
					next := raw[k:l]
					if next == "5" {
						// skip "48;5;N"
						m := l + 1
						for m < end && raw[m] != ';' {
							m++
						}
						i = m + 1
						continue
					} else if next == "2" {
						// skip "48;2;R;G;B"
						m := l + 1
						for count := 0; count < 3 && m < end; count++ {
							for m < end && raw[m] != ';' {
								m++
							}
							m++
						}
						i = m
						continue
					}
				}
			}

			// decide whether to keep this token
			// manually parse ASCII digits to int
			isNum := true
			val := 0
			for p := i; p < j; p++ {
				c := raw[p]
				if c < '0' || c > '9' {
					isNum = false
					break
				}
				val = val*10 + int(c-'0')
			}
			// also drop plain ANSI (30-47/90-107) background codes, which is
			// the form a low-color-profile newBg (or a source run rendered at
			// a different profile) can use instead of "48;..."
			keep := !isNum ||
				((val < 40 || val > 47) && (val < 100 || val > 107) && val != 49)

			if keep {
				if sb.Len() > 0 {
					sb.WriteByte(';')
				}
				sb.WriteString(token)
			}
			// advance past this token (and the semicolon)
			i = j + 1
		}

		// append our new background, if the profile actually renders one
		// (Ascii/NoColor yields "" — nothing to append).
		if newBg != "" {
			if sb.Len() > 0 {
				sb.WriteByte(';')
			}
			sb.WriteString(newBg)
		}

		return "\x1b[" + sb.String() + "m"
	})
}
