package jsonutil

import (
	"encoding/json"
	"testing"
)

type detailFixture struct {
	AnnualReturns       []int          `json:"annual_returns"`
	HistoricalSnapshots []string       `json:"historical_snapshots"`
	ReferencingPlans    []string       `json:"referencing_plans"`
	SimulationWindow    map[string]any `json:"simulation_window"`
	Nested              *nestedFixture `json:"nested,omitempty"`
}

type nestedFixture struct {
	Checks []string `json:"checks"`
}

func TestNonNilSlicesStructFields(t *testing.T) {
	v := detailFixture{
		SimulationWindow: map[string]any{"excluded_years": []int(nil)},
		Nested:           &nestedFixture{},
	}
	NonNilSlices(&v)

	if v.AnnualReturns == nil || len(v.AnnualReturns) != 0 {
		t.Fatalf("annual_returns = %#v, want empty slice", v.AnnualReturns)
	}
	if v.HistoricalSnapshots == nil {
		t.Fatal("historical_snapshots is nil")
	}
	if v.ReferencingPlans == nil {
		t.Fatal("referencing_plans is nil")
	}
	if v.Nested.Checks == nil {
		t.Fatal("nested.checks is nil")
	}
	excluded, ok := v.SimulationWindow["excluded_years"].([]int)
	if !ok || excluded == nil {
		t.Fatalf("excluded_years = %#v, want empty []int", v.SimulationWindow["excluded_years"])
	}
}

func TestNonNilSlicesMapEnvelope(t *testing.T) {
	payload := map[string]any{
		"instruments": ([]string)(nil),
		"simulation_window": map[string]any{
			"selected_years": []int(nil),
		},
	}
	NonNilSlices(payload)

	instruments, ok := payload["instruments"].([]string)
	if !ok || instruments == nil {
		t.Fatalf("instruments = %#v", payload["instruments"])
	}
	window, ok := payload["simulation_window"].(map[string]any)
	if !ok {
		t.Fatal("simulation_window missing")
	}
	years, ok := window["selected_years"].([]int)
	if !ok || years == nil {
		t.Fatalf("selected_years = %#v", window["selected_years"])
	}
}

func TestNonNilSlicesJSONEncodesEmptyArrays(t *testing.T) {
	v := detailFixture{
		SimulationWindow: map[string]any{"excluded_years": []int(nil)},
	}
	NonNilSlices(&v)
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"annual_returns":[]`, `"historical_snapshots":[]`, `"excluded_years":[]`} {
		if !contains(s, want) {
			t.Fatalf("json %q missing %q", s, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
