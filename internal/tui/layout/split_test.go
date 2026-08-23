package layout

import "testing"

func TestSplitWidthsBothPanels(t *testing.T) {
	left, right := splitWidths(100, 0.7, true, true, 0)
	if left != 70 || right != 30 {
		t.Fatalf("expected 70/30, got %d/%d", left, right)
	}
	if left+right != 100 {
		t.Fatalf("widths must sum to total: %d+%d", left, right)
	}
}

func TestSplitWidthsCollapsesWhenNarrow(t *testing.T) {
	// Below the threshold the right panel collapses to a single column.
	left, right := splitWidths(60, 0.7, true, true, 80)
	if left != 60 || right != 0 {
		t.Fatalf("narrow layout should be single column 60/0, got %d/%d", left, right)
	}
}

func TestSplitWidthsAtThresholdKeepsSplit(t *testing.T) {
	// Exactly at the threshold is not "below", so the split is retained.
	left, right := splitWidths(80, 0.7, true, true, 80)
	if right == 0 {
		t.Fatalf("at threshold the split should be retained, got %d/%d", left, right)
	}
}

func TestSplitWidthsSinglePanels(t *testing.T) {
	if l, r := splitWidths(120, 0.7, true, false, 80); l != 120 || r != 0 {
		t.Fatalf("left-only should take full width, got %d/%d", l, r)
	}
	if l, r := splitWidths(120, 0.7, false, true, 80); l != 0 || r != 120 {
		t.Fatalf("right-only should take full width, got %d/%d", l, r)
	}
	if l, r := splitWidths(120, 0.7, false, false, 80); l != 0 || r != 0 {
		t.Fatalf("no panels should be 0/0, got %d/%d", l, r)
	}
}

func TestSplitWidthsDisabledCollapse(t *testing.T) {
	// collapseBelow == 0 never collapses, even at tiny widths.
	left, right := splitWidths(20, 0.7, true, true, 0)
	if right == 0 {
		t.Fatalf("with collapse disabled the right panel should remain, got %d/%d", left, right)
	}
}
