package repository_test

import (
	"context"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestEnsureSystemDefaultIdempotent(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewAssumptionProfileRepo(db)
	ctx := context.Background()

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("ensure system default: %v", err)
	}
	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("second ensure system default: %v", err)
	}

	got, err := repo.Get(ctx, assumptions.SystemProfileID, assumptions.SystemProfileVersion)
	if err != nil {
		t.Fatalf("get system profile: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("persisted system profile invalid: %v", err)
	}
	want := assumptions.SystemDefaultProfile()
	wantHash, _ := want.ContentHash()
	gotHash, _ := got.ContentHash()
	if wantHash != gotHash {
		t.Fatalf("round-tripped profile hash mismatch:\n want %s\n got  %s", wantHash, gotHash)
	}

	summaries, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected exactly one profile row, got %d", len(summaries))
	}
	if summaries[0].OwnerScope != assumptions.OwnerSystem || summaries[0].Status != assumptions.StatusActive {
		t.Fatalf("unexpected system summary: %+v", summaries[0])
	}
}

func TestPreferencesFallBackToSystem(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewAssumptionProfileRepo(db)
	ctx := context.Background()

	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != assumptions.SystemProfileID ||
		pref.DefaultProfileVersion != assumptions.SystemProfileVersion ||
		pref.DefaultScenario != assumptions.ScenarioBaseline {
		t.Fatalf("expected system default preference, got %+v", pref)
	}

	if err := repo.SetPreferences(ctx, repository.AssumptionPreferences{
		DefaultProfileID: "user_cma", DefaultProfileVersion: 3, DefaultScenario: assumptions.ScenarioConservative,
	}); err != nil {
		t.Fatalf("set preferences: %v", err)
	}
	got, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("re-get preferences: %v", err)
	}
	if got.DefaultProfileID != "user_cma" || got.DefaultProfileVersion != 3 ||
		got.DefaultScenario != assumptions.ScenarioConservative {
		t.Fatalf("preferences not persisted: %+v", got)
	}
}

func TestSaveAndActivateDraftProfile(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewAssumptionProfileRepo(db)
	ctx := context.Background()
	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("ensure system: %v", err)
	}

	draft := assumptions.SystemDefaultProfile()
	draft.ID = "user_cma"
	draft.Version = 1
	draft.OwnerScope = assumptions.OwnerUser
	draft.Status = assumptions.StatusDraft
	draft.Name = "我的自定义假设"
	if err := repo.Save(ctx, draft, "copied from system", "tester", "2026-06-20"); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := repo.Activate(ctx, "user_cma", 1); err != nil {
		t.Fatalf("activate draft: %v", err)
	}
	active, err := repo.GetActiveLatest(ctx, "user_cma")
	if err != nil {
		t.Fatalf("get active latest: %v", err)
	}
	if active.Ref() != "user_cma@1" {
		t.Fatalf("unexpected active ref %q", active.Ref())
	}
}
