package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ContrastRatio computes the WCAG 2.1 contrast ratio between two hex colors.
// Values range from 1 (no contrast) to
// 21 (black on white).
func ContrastRatio(hexA, hexB string) (float64, error) {
	la, err := relativeLuminance(hexA)
	if err != nil {
		return 0, fmt.Errorf("color %q: %w", hexA, err)
	}
	lb, err := relativeLuminance(hexB)
	if err != nil {
		return 0, fmt.Errorf("color %q: %w", hexB, err)
	}
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05), nil
}

// relativeLuminance implements the WCAG relative luminance formula for an
// sRGB hex color (with or without a leading '#').
func relativeLuminance(hex string) (float64, error) {
	r, g, b, err := hexToRGB(hex)
	if err != nil {
		return 0, err
	}
	lin := func(c uint8) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b), nil
}

func hexToRGB(hex string) (r, g, b uint8, err error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("expected a 6-digit hex color, got %q", hex)
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q: %w", hex, err)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), nil
}
