//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

const maxSeedString = "9223372036854775807"

func TestMaxSeedRoundTripIntegration(t *testing.T) {
	srv, db, client, _ := setupFullStackIntegration(t)
	planID := seedSimulationReadyPlan(t, db)

	paramsResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/parameters")
	if err != nil {
		t.Fatal(err)
	}
	paramsEnv := decodeEnvelope(t, readBody(t, paramsResp))
	params := paramsEnv["data"].(map[string]any)["parameters"].(map[string]any)
	plan, err := repository.NewPlanRepo(db).GetByID(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}

	params["seed"] = maxSeedString
	updateBody, _ := json.Marshal(map[string]any{
		"config_version": plan.ConfigVersion,
		"parameters":     params,
	})
	upReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/plans/"+planID+"/parameters", bytes.NewReader(updateBody))
	upReq.Header.Set("Content-Type", "application/json")
	upResp, err := client.Do(upReq)
	if err != nil {
		t.Fatal(err)
	}
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("update parameters status=%d body=%s", upResp.StatusCode, readBody(t, upResp))
	}
	upEnv := decodeEnvelope(t, readBody(t, upResp))
	savedSeed := upEnv["data"].(map[string]any)["parameters"].(map[string]any)["seed"].(string)
	if savedSeed != maxSeedString {
		t.Fatalf("saved seed=%q want %q", savedSeed, maxSeedString)
	}

	getResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/parameters")
	if err != nil {
		t.Fatal(err)
	}
	getEnv := decodeEnvelope(t, readBody(t, getResp))
	gotSeed := getEnv["data"].(map[string]any)["parameters"].(map[string]any)["seed"].(string)
	if gotSeed != maxSeedString {
		t.Fatalf("queried seed=%q want %q", gotSeed, maxSeedString)
	}

	simBody, _ := json.Marshal(map[string]any{"runs": 1000, "seed": maxSeedString})
	simReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/simulations", bytes.NewReader(simBody))
	simReq.Header.Set("Content-Type", "application/json")
	simResp, err := client.Do(simReq)
	if err != nil {
		t.Fatal(err)
	}
	if simResp.StatusCode != http.StatusOK {
		t.Fatalf("create simulation status=%d body=%s", simResp.StatusCode, readBody(t, simResp))
	}
	simEnv := decodeEnvelope(t, readBody(t, simResp))
	runID := simEnv["data"].(map[string]any)["run_id"].(string)
	jobID := simEnv["data"].(map[string]any)["job_id"].(string)
	waitJobSucceeded(t, srv, jobID)

	runResp, err := client.Get(srv.URL + "/api/v1/simulations/" + runID)
	if err != nil {
		t.Fatal(err)
	}
	runEnv := decodeEnvelope(t, readBody(t, runResp))
	runSeed := runEnv["data"].(map[string]any)["seed"].(string)
	if runSeed != maxSeedString {
		t.Fatalf("run seed=%q want %q", runSeed, maxSeedString)
	}

	pathResp, err := client.Get(srv.URL + "/api/v1/simulations/" + runID + "/paths/0")
	if err != nil {
		t.Fatal(err)
	}
	if pathResp.StatusCode != http.StatusOK {
		t.Fatalf("path detail status=%d body=%s", pathResp.StatusCode, readBody(t, pathResp))
	}
	pathEnv := decodeEnvelope(t, readBody(t, pathResp))
	pathSeed := pathEnv["data"].(map[string]any)["path_seed"].(string)
	if pathSeed == "" {
		t.Fatal("expected non-empty path_seed")
	}
	if pathSeed == "0" {
		t.Fatal("path_seed must not be silent zero fallback")
	}

	pathsResp, err := client.Get(srv.URL + "/api/v1/simulations/" + runID + "/paths")
	if err != nil {
		t.Fatal(err)
	}
	pathsEnv := decodeEnvelope(t, readBody(t, pathsResp))
	paths := pathsEnv["data"].(map[string]any)["paths"].([]any)
	if len(paths) == 0 {
		t.Fatal("expected path index rows")
	}
	// The list is ordered by representative percentile, so locate path 0
	// explicitly before comparing its stored seed to the regenerated detail.
	indexSeed := ""
	for _, p := range paths {
		row := p.(map[string]any)
		if int(row["path_no"].(float64)) == 0 {
			indexSeed = row["path_seed"].(string)
			break
		}
	}
	if indexSeed == "" {
		t.Fatal("path 0 not found in path index")
	}
	if indexSeed != pathSeed {
		t.Fatalf("path seed mismatch index=%q detail=%q", indexSeed, pathSeed)
	}
	if indexSeed[0] == '-' {
		t.Fatalf("path seed must be non-negative, got %q", indexSeed)
	}
}

func TestInvalidSeedRejectedIntegration(t *testing.T) {
	srv, db, client, _ := setupFullStackIntegration(t)
	planID := seedSimulationReadyPlan(t, db)

	paramsResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/parameters")
	if err != nil {
		t.Fatal(err)
	}
	paramsEnv := decodeEnvelope(t, readBody(t, paramsResp))
	params := paramsEnv["data"].(map[string]any)["parameters"].(map[string]any)
	plan, err := repository.NewPlanRepo(db).GetByID(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}

	cases := []string{"-1", "1.5", "1e10", "9223372036854775808"}
	for _, badSeed := range cases {
		t.Run(badSeed, func(t *testing.T) {
			params["seed"] = badSeed
			updateBody, _ := json.Marshal(map[string]any{
				"config_version": plan.ConfigVersion,
				"parameters":     params,
			})
			upReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/plans/"+planID+"/parameters",
				bytes.NewReader(updateBody))
			upReq.Header.Set("Content-Type", "application/json")
			upResp, err := client.Do(upReq)
			if err != nil {
				t.Fatal(err)
			}
			if upResp.StatusCode == http.StatusOK {
				t.Fatalf("expected error for seed %q", badSeed)
			}
			assertErrorCode(t, readBody(t, upResp), "parameters_invalid")
		})
	}
}
