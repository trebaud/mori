package tui

import "testing"

func TestColWidths_PRColumnHiddenBelowBreakpoint(t *testing.T) {
	for _, w := range []int{60, 80, 100, 109} {
		_, _, _, _, prW := colWidths(w)
		if prW != 0 {
			t.Errorf("width=%d: expected prW=0, got %d", w, prW)
		}
	}
}

func TestColWidths_PRColumnVisibleAtBreakpoint(t *testing.T) {
	for _, w := range []int{110, 130, 200} {
		_, _, _, _, prW := colWidths(w)
		if prW == 0 {
			t.Errorf("width=%d: expected prW>0, got 0", w)
		}
	}
}

func TestColWidths_BranchHasMinimum(t *testing.T) {
	branchW, _, _, _, _ := colWidths(60)
	if branchW < 20 {
		t.Errorf("expected branchW >= 20, got %d", branchW)
	}
}
