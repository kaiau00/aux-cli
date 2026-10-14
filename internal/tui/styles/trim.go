package styles

import (
	"strings"
	"unicode/utf8"
)

// TrimTrailingSpace removes the padding lipgloss leaves on the right of every
// line it renders to a width, without disturbing the escape sequences around
// it.
//
// This is not tidiness. A line padded out to exactly the terminal's width is
// the worst possible width for a line to be:
//
//   - Bubble Tea's inline renderer appends an erase-to-end-of-line only when a
//     line is *shorter* than the terminal ("if ansi.StringWidth(line) <
//     r.width"). A full-width line therefore never clears what was under it,
//     so anything left from the previous frame on that row survives.
//   - It sits exactly on the wrap boundary. Make the window one column
//     narrower and every such line becomes two physical rows while the
//     renderer still counts one, so its cursor-up lands a row short and it
//     paints over the wrong rows, leaving a stale copy of the one it missed.
//   - Printed lines keep that padding in the user's scrollback for good, and
//     it comes back when they select and copy it.
//
// Trimming costs nothing: Aux paints no background (D17), so the padding was
// never carrying any colour.
//
// A plain strings.TrimRight does not work here. lipgloss closes a styled run
// *after* its padding, so the line ends in an escape sequence rather than in
// the spaces, and TrimRight finds nothing to remove.
func TrimTrailingSpace(s string) string {
	if !strings.Contains(s, " ") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = trimLineTrailingSpace(line)
	}
	return strings.Join(lines, "\n")
}

// trimLineTrailingSpace drops the spaces that follow the last visible
// character on a line, keeping every escape sequence, in order.
func trimLineTrailingSpace(line string) string {
	var out, pending strings.Builder
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			end := escapeEnd(line, i)
			pending.WriteString(line[i:end])
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		i += size
		if r == ' ' {
			pending.WriteRune(r)
			continue
		}
		// Something visible: whatever was pending was interior, so it stays.
		out.WriteString(pending.String())
		pending.Reset()
		out.WriteRune(r)
	}
	// What is pending now trails the last visible character. The spaces go;
	// the escapes stay, so the run is still closed.
	for i := 0; i < pending.Len(); {
		tail := pending.String()
		if tail[i] == 0x1b {
			end := escapeEnd(tail, i)
			out.WriteString(tail[i:end])
			i = end
			continue
		}
		_, size := utf8.DecodeRuneInString(tail[i:])
		i += size
	}
	return out.String()
}

// escapeEnd returns the index just past the escape sequence starting at i.
// It covers the two forms lipgloss and glamour emit: CSI, which ends at a byte
// in @ to ~, and OSC, which ends at BEL or at a string terminator.
func escapeEnd(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[':
		for j := i + 2; j < len(s); j++ {
			if s[j] >= '@' && s[j] <= '~' {
				return j + 1
			}
		}
	case ']':
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
	default:
		return i + 2
	}
	return len(s)
}
