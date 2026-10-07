package chat

import "testing"

// Both arguments are whole-second Unix timestamps: msg.CreatedAt comes from
// strftime('%s', 'now') and Finish.Time from time.Now().Unix(). The previous
// implementation divided by 1000 "to convert to seconds", so every duration in
// the transcript read a thousand times too small. Replies in this repository's
// own database run 0-46s with a median of 2s, which is precisely the range the
// bug made meaningless: a 46-second wait was reported as "46ms".
func TestFormatSecondsDiffReadsSecondsAsSeconds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end int64
		want       string
	}{
		{"same second", 1000, 1000, "<1s"},
		{"two seconds, the real median", 1000, 1002, "2s"},
		{"forty-six seconds, the real maximum", 1000, 1046, "46s"},
		{"fifty-nine seconds stays in seconds", 1000, 1059, "59s"},
		{"a minute exactly", 1000, 1060, "1m 0s"},
		{"ninety seconds", 1000, 1090, "1m 30s"},
		{"an hour", 1000, 4600, "60m 0s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatSecondsDiff(tc.start, tc.end); got != tc.want {
				t.Errorf("formatSecondsDiff(%d, %d) = %q, want %q",
					tc.start, tc.end, got, tc.want)
			}
		})
	}
}

// A finish stamp before the creation stamp means a clock change or a malformed
// row. The old code printed the raw difference, which is how a fixture here
// produced "(-1791389564ms)" in the transcript.
func TestFormatSecondsDiffNeverReportsNegativeTime(t *testing.T) {
	if got := formatSecondsDiff(2000, 1000); got != "<1s" {
		t.Errorf("a finish before the start gave %q; want it clamped, not negative", got)
	}
}
