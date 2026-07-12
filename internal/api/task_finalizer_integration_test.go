package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/service"
	taskcore "github.com/fireman/fireman/internal/task"
)

type finalizedTask struct {
	TaskID    string
	Token     string
	ResultKey string
	Result    service.TaskFinalizeResult
}

func finalizeExternalTask(t *testing.T, st internalStack, taskID string, raw []byte) finalizedTask {
	t.Helper()
	ctx := context.Background()
	token := "finalize-token-" + taskID
	owner := taskcore.ClaimRequest{
		WorkerType: repository.WorkerTypeSidecar,
		WorkerID:   "sidecar_worker:integration",
		ClaimToken: token,
	}
	if _, err := st.coordinator.Claim(ctx, taskID, owner); err != nil {
		t.Fatalf("claim %s: %v", taskID, err)
	}
	compressed, err := resourcedb.GzipBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := st.resources.InsertContent(
		ctx, "application/json", "gzip", resourcedb.SupportedSchemaVersion,
		compressed, time.Now(), 7*24*time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	resultKey := "resource:" + envelope.ResourceKey
	if _, err := st.coordinator.Report(ctx, taskID, taskcore.ResultRequest{
		WorkerType: owner.WorkerType, WorkerID: owner.WorkerID, ClaimToken: token,
		Outcome: "success", ResultKey: resultKey, ResultMeta: meta,
	}); err != nil {
		t.Fatalf("report %s: %v", taskID, err)
	}
	reservations, err := st.coordinator.ReserveDueFinalizations(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, reservation := range reservations {
		if reservation.Task.ID != taskID {
			continue
		}
		result := st.finalizer.Finalize(ctx, taskID, reservation.ReservationEnds)
		if result.Result != service.TaskFinalizeSuccess {
			_, finishErr := st.coordinator.FinishFinalizationFailure(
				ctx, taskID, reservation.ReservationEnds,
				result.Result == service.TaskFinalizeRetryableError,
				result.ErrorCode, result.ErrorMessage,
			)
			if finishErr != nil {
				t.Fatalf("finish failed finalization %s: %v", taskID, finishErr)
			}
		}
		return finalizedTask{TaskID: taskID, Token: token, ResultKey: resultKey, Result: result}
	}
	t.Fatalf("task %s was not reserved for finalization", taskID)
	return finalizedTask{}
}

func TestTaskFinalizerDirectoryProtocolAndBusinessCommit(t *testing.T) {
	st := newInternalStack(t)
	created, err := st.assets.SyncDirectory(context.Background(), service.DirectorySyncRequest{
		SyncKey: "hk_stock",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created.Tasks[0].Task.ID
	raw, _ := json.Marshal(map[string]any{
		"type": "asset_directory_sync", "sync_key": "hk_stock", "scope": "hk_all",
		"assets": []map[string]any{
			{
				"market": "HK", "instrument_type": "hk_stock", "symbol": "00700",
				"name": "Tencent", "instrument_kind": "stock", "currency": "HKD",
				"source_name": "integration", "source_as_of": "2026-07-12",
			},
			{
				"market": "HK", "instrument_type": "hk_stock", "symbol": "00005",
				"name": "HSBC", "instrument_kind": "stock", "currency": "HKD",
				"source_name": "integration", "source_as_of": "2026-07-12",
			},
		},
	})
	finalized := finalizeExternalTask(t, st, taskID, raw)
	if finalized.Result.Result != service.TaskFinalizeSuccess {
		t.Fatalf("finalization=%+v", finalized.Result)
	}
	assertTaskAndBusinessVersion(t, st, taskID, "asset_directory|hk_stock")
	if got := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE instrument_type='hk_stock' AND active=1`); got != 2 {
		t.Fatalf("active hk_stock count=%d", got)
	}
}

func TestTaskFinalizerHistoryProtocolAndDuplicateResult(t *testing.T) {
	st := newInternalStack(t)
	seed := cnETFAssetSeed()
	seed.Points = nil
	seedMarketAssetWithHistory(t, st.db, seed)
	created, err := st.assets.SyncHistory(context.Background(), service.HistorySyncRequest{
		AssetKey: seed.AssetKey, Mode: "default_refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	dates := sequentialTaskDates("2024-01-01", 40)
	points := make([]map[string]any, len(dates))
	for i, date := range dates {
		points[i] = map[string]any{"date": date, "value": 100 + float64(i)}
	}
	raw, _ := json.Marshal(map[string]any{
		"type": "asset_history_sync", "asset_key": seed.AssetKey,
		"adjust_policy": "hfq", "point_type": "adjusted_close",
		"source_name": "integration", "points": points,
	})
	finalized := finalizeExternalTask(t, st, created.Task.ID, raw)
	if finalized.Result.Result != service.TaskFinalizeSuccess {
		t.Fatalf("finalization=%+v", finalized.Result)
	}
	assertTaskAndBusinessVersion(t, st, created.Task.ID,
		"asset_history|"+seed.AssetKey+"|hfq|adjusted_close")
	if got := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_points WHERE asset_key=?`, seed.AssetKey); got != 40 {
		t.Fatalf("history point count=%d", got)
	}
	if got := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_detail_projections WHERE asset_key=?`, seed.AssetKey); got != 1 {
		t.Fatalf("detail projection count=%d", got)
	}

	duplicate, err := st.coordinator.Report(context.Background(), created.Task.ID, taskcore.ResultRequest{
		WorkerType: repository.WorkerTypeSidecar, WorkerID: "sidecar_worker:integration",
		ClaimToken: finalized.Token, Outcome: "success", ResultKey: finalized.ResultKey,
	})
	if err != nil || duplicate.Status != repository.WorkerTaskStatusComplete {
		t.Fatalf("duplicate result task=%+v err=%v", duplicate, err)
	}
	if got := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_points WHERE asset_key=?`, seed.AssetKey); got != 40 {
		t.Fatalf("duplicate result changed points: %d", got)
	}
}

func TestTaskFinalizerFXProtocolAndBusinessCommit(t *testing.T) {
	st := newInternalStack(t)
	created, err := st.assets.SyncFXRates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"type": "fx_rate_sync", "pairs": []string{"HKDCNY", "USDCNY"},
		"source_name": "integration",
		"rates": []map[string]any{
			{"date": "2026-07-11", "pair": "USDCNY", "value": 7.21},
			{"date": "2026-07-12", "pair": "USDCNY", "value": 7.22},
			{"date": "2026-07-11", "pair": "HKDCNY", "value": 0.92},
			{"date": "2026-07-12", "pair": "HKDCNY", "value": 0.93},
		},
	})
	finalized := finalizeExternalTask(t, st, created.Task.ID, raw)
	if finalized.Result.Result != service.TaskFinalizeSuccess {
		t.Fatalf("finalization=%+v", finalized.Result)
	}
	assertTaskAndBusinessVersion(t, st, created.Task.ID, "fx_rate|USDCNY")
	if got := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_data_versions WHERE version_key='fx_rate|HKDCNY' AND task_id=?`,
		created.Task.ID); got != 1 {
		t.Fatalf("HKDCNY version count=%d", got)
	}
	for _, instrumentID := range []string{"system_fx_usdcny", "system_fx_hkdcny"} {
		if got := countRows(t, st.db,
			`SELECT COUNT(*) FROM market_data_points WHERE instrument_id=? AND point_type='fx_rate'`,
			instrumentID); got != 2 {
			t.Fatalf("%s point count=%d", instrumentID, got)
		}
	}
}

func assertTaskAndBusinessVersion(t *testing.T, st internalStack, taskID, versionKey string) {
	t.Helper()
	var status string
	if err := st.db.QueryRow(`SELECT status FROM worker_tasks WHERE id=?`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != repository.WorkerTaskStatusComplete {
		t.Fatalf("task %s status=%s", taskID, status)
	}
	if got := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_data_versions WHERE version_key=? AND task_id=?`,
		versionKey, taskID); got != 1 {
		t.Fatalf("version %s for task %s count=%d", versionKey, taskID, got)
	}
	if got := countRows(t, st.db,
		`SELECT COUNT(*) FROM worker_task_finalize_records WHERE task_id=? AND result='success'`, taskID); got != 1 {
		t.Fatalf("finalize record for task %s count=%d", taskID, got)
	}
}

func sequentialTaskDates(start string, count int) []string {
	first, err := time.Parse("2006-01-02", start)
	if err != nil {
		panic(fmt.Sprintf("invalid test date: %v", err))
	}
	out := make([]string, count)
	for i := range out {
		out[i] = first.AddDate(0, 0, i).Format("2006-01-02")
	}
	return out
}
