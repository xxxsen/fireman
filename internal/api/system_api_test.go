package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireman/fireman/internal/testutil"
)

func TestSystemBackupDownload(t *testing.T) {
	db, dbPath := testutil.OpenTestDBPath(t)
	services := NewServices(db, dbPath, "", nil)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, DBPath: dbPath, Services: services}))
	defer srv.Close()

	planBody := []byte(`{"name":"backup-test","valuation_date":"2024-01-01"}`)
	resp, err := http.Post(srv.URL+"/api/v1/plans", "application/json", bytes.NewReader(planBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create plan status=%d", resp.StatusCode)
	}

	backupResp, err := http.Get(srv.URL + "/api/v1/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	if backupResp.StatusCode != http.StatusOK {
		t.Fatalf("backup status=%d body=%s", backupResp.StatusCode, readBody(t, backupResp))
	}
	backupData, err := io.ReadAll(backupResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(backupData) == 0 {
		t.Fatal("empty backup")
	}

	badReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/system/restore", bytes.NewReader([]byte("not-sqlite")))
	badReq.Header.Set("Content-Type", "application/octet-stream")
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = badResp.Body.Close() }()
	if badResp.StatusCode == http.StatusOK {
		t.Fatal("expected invalid backup to fail")
	}
}

func TestPlanExportEndpoints(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db, "")}))
	defer srv.Close()

	for _, path := range []string{
		"/api/v1/plans/" + planID + "/export/json",
		"/api/v1/plans/" + planID + "/export/targets.csv",
		"/api/v1/plans/" + planID + "/export/rebalance.csv",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, readBody(t, resp))
		}
	}
}
