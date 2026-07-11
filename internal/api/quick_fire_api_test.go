package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func quickFireBody() map[string]any {
	return map[string]any{
		"base_currency": "CNY", "current_age": 40, "planned_fire_age": 40, "end_age": 60,
		"current_assets_minor": 120001, "annual_savings_minor": 0, "annual_savings_growth_rate": 0,
		"annual_spending_minor": 12000, "annual_retirement_income_minor": 6000,
		"annual_retirement_income_growth_rate": 0, "annual_return_rate": 0, "inflation_rate": 0,
		"terminal_wealth_floor_minor": 0,
	}
}

func TestQuickFIREAPI(t *testing.T) {
	srv := testRouter(t)
	defer srv.Close()
	body, err := json.Marshal(quickFireBody())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Post(srv.URL+"/api/v1/fire/quick-calculations", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, mustRead(t, resp))
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	data := env["data"].(map[string]any)
	if data["engine_version"] != "quick_fire_v1" || data["outcome_status"] != "sustainable" {
		t.Fatalf("unexpected result: %+v", data)
	}
	if data["terminal_wealth_minor"] != float64(1) {
		t.Fatalf("terminal wealth=%v, want 1", data["terminal_wealth_minor"])
	}
}

func TestQuickFIREAPIRejectsStrictInvalidBodies(t *testing.T) {
	srv := testRouter(t)
	defer srv.Close()
	cases := []string{
		`{"base_currency":"CNY","unknown":1}`,
		`{"base_currency":"CNY"}{}`,
		`{"base_currency":"CNY","annual_return_rate":"bad"}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			resp, err := srv.Client().Post(srv.URL+"/api/v1/fire/quick-calculations", "application/json", strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", resp.StatusCode, mustRead(t, resp))
			}
			assertErrorCode(t, mustRead(t, resp), "quick_fire_parameters_invalid")
		})
	}
}

func TestQuickFIREAPIRejectsFractionalMinorAndOversizedBody(t *testing.T) {
	srv := testRouter(t)
	defer srv.Close()
	body := quickFireBody()
	body["current_assets_minor"] = 1.5
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Post(srv.URL+"/api/v1/fire/quick-calculations", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("fractional status=%d", resp.StatusCode)
	}
	assertErrorCode(t, mustRead(t, resp), "quick_fire_parameters_invalid")

	resp, err = srv.Client().Post(srv.URL+"/api/v1/fire/quick-calculations", "application/json", strings.NewReader("{"+strings.Repeat("a", quickFireRequestMaxBytes)+"}"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("large body status=%d", resp.StatusCode)
	}
	assertErrorCode(t, mustRead(t, resp), "quick_fire_parameters_invalid")
}

func TestQuickFIREAPIBoundariesAndRangeError(t *testing.T) {
	srv := testRouter(t)
	defer srv.Close()
	post := func(t *testing.T, body map[string]any) (int, []byte) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Post(srv.URL+"/api/v1/fire/quick-calculations", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, mustRead(t, resp)
	}
	for _, tc := range []struct {
		field  string
		values []float64
	}{
		{field: "annual_return_rate", values: []float64{-0.99, 0, 1}},
		{field: "inflation_rate", values: []float64{-0.02, 0, 0.2}},
	} {
		for _, value := range tc.values {
			body := quickFireBody()
			body[tc.field] = value
			status, raw := post(t, body)
			if status != http.StatusOK {
				t.Fatalf("%s=%v status=%d body=%s", tc.field, value, status, raw)
			}
		}
	}
	for _, tc := range []struct {
		field string
		value any
	}{
		{field: "annual_return_rate", value: -0.991},
		{field: "annual_return_rate", value: 1.001},
		{field: "inflation_rate", value: -0.021},
		{field: "inflation_rate", value: 0.201},
		{field: "end_age", value: 121},
		{field: "planned_fire_age", value: 60},
	} {
		body := quickFireBody()
		body[tc.field] = tc.value
		status, raw := post(t, body)
		if status != http.StatusBadRequest {
			t.Fatalf("%s=%v status=%d body=%s", tc.field, tc.value, status, raw)
		}
		assertErrorCode(t, raw, "quick_fire_parameters_invalid")
	}
	body := quickFireBody()
	body["current_age"] = 18
	body["planned_fire_age"] = 18
	body["end_age"] = 120
	body["current_assets_minor"] = 999_999_999_999_00
	body["annual_return_rate"] = 1
	status, raw := post(t, body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("overflow status=%d body=%s", status, raw)
	}
	assertErrorCode(t, raw, "quick_fire_result_out_of_range")
}
