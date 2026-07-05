package service

import "testing"

// TestDefaultParametersCurrentAge verifies that non-wizard plan creation must
// default current age to 35 so the parameters page matches the wizard default.
func TestDefaultParametersCurrentAge(t *testing.T) {
	if got := defaultParameters("pln_test", nil).CurrentAge; got != 35 {
		t.Fatalf("default CurrentAge = %d, want 35", got)
	}
}
