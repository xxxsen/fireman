package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOKNormalizesNilSlices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	payload := map[string]any{
		"historical_snapshots": ([]string)(nil),
		"referencing_plans":    ([]string)(nil),
		"simulation_window": map[string]any{
			"excluded_years": []int(nil),
		},
	}
	OK(c, payload)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["historical_snapshots"] == nil {
		t.Fatal("historical_snapshots is null")
	}
	if body.Data["referencing_plans"] == nil {
		t.Fatal("referencing_plans is null")
	}
	window, ok := body.Data["simulation_window"].(map[string]any)
	if !ok {
		t.Fatal("simulation_window missing")
	}
	if window["excluded_years"] == nil {
		t.Fatal("excluded_years is null")
	}
}
