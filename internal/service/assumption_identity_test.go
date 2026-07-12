package service

import (
	"context"
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func saveUserProfileVersion(
	t *testing.T, repo *repository.AssumptionProfileRepo, id string, version int,
) assumptions.Profile {
	t.Helper()
	p := assumptions.SystemDefaultProfile()
	p.ID = id
	p.Version = version
	p.OwnerScope = assumptions.OwnerUser
	p.Name = id
	p.Status = assumptions.StatusDraft
	if err := repo.Save(context.Background(), p, "internal test", "tester", "2026-07-12"); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAssumptionActivationMigratesDefaultAndKeepsSupersededPin(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)
	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatal(err)
	}
	saveUserProfileVersion(t, repo, "user_x", 1)
	if _, err := repo.Activate(ctx, "user_x", 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPreferences(ctx, repository.AssumptionPreferences{
		DefaultProfileID: "user_x", DefaultProfileVersion: 1,
		DefaultScenario: assumptions.ScenarioBaseline,
	}); err != nil {
		t.Fatal(err)
	}
	saveUserProfileVersion(t, repo, "user_x", 2)
	migrated, err := repo.Activate(ctx, "user_x", 2)
	if err != nil || !migrated {
		t.Fatalf("migrated=%v err=%v", migrated, err)
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pref.DefaultProfileID != "user_x" || pref.DefaultProfileVersion != 2 {
		t.Fatalf("preferences=%+v", pref)
	}
	v1, err := repo.Get(ctx, "user_x", 1)
	if err != nil || v1.Status != assumptions.StatusSuperseded {
		t.Fatalf("v1=%+v err=%v", v1, err)
	}
	resolved, scenario, _, err := resolveProfileAndScenario(ctx, repo, repository.PlanParameters{
		AssumptionSelectionMode: SelectionPinnedProfile,
		ReturnAssumptionSetID:   "user_x", ReturnAssumptionSetVersion: 1,
		ReturnAssumptionScenario: assumptions.ScenarioBaseline,
	})
	if err != nil || resolved.Version != 1 || scenario != assumptions.ScenarioBaseline {
		t.Fatalf("resolved=%+v scenario=%s err=%v", resolved, scenario, err)
	}
}

func TestResolveAssumptionProfileAndScenarioCombinations(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)
	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPreferences(ctx, repository.AssumptionPreferences{
		DefaultProfileID:      assumptions.SystemProfileID,
		DefaultProfileVersion: assumptions.SystemProfileVersion,
		DefaultScenario:       assumptions.ScenarioConservative,
	}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		selection string
		scenario  string
		want      string
	}{
		{"global-global", SelectionFollowGlobal, assumptions.ScenarioFollowGlobal, assumptions.ScenarioConservative},
		{"global-explicit", SelectionFollowGlobal, assumptions.ScenarioBaseline, assumptions.ScenarioBaseline},
		{"pinned-global", SelectionPinnedProfile, assumptions.ScenarioFollowGlobal, assumptions.ScenarioConservative},
		{"pinned-explicit", SelectionPinnedProfile, assumptions.ScenarioOptimistic, assumptions.ScenarioOptimistic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := repository.PlanParameters{
				AssumptionSelectionMode: tc.selection, ReturnAssumptionScenario: tc.scenario,
			}
			if tc.selection == SelectionPinnedProfile {
				params.ReturnAssumptionSetID = assumptions.SystemProfileID
				params.ReturnAssumptionSetVersion = assumptions.SystemProfileVersion
			}
			_, got, _, err := resolveProfileAndScenario(ctx, repo, params)
			if err != nil || got != tc.want {
				t.Fatalf("scenario=%s want=%s err=%v", got, tc.want, err)
			}
		})
	}
}

func TestResolvePinnedDraftIsRejected(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewAssumptionProfileRepo(db)
	saveUserProfileVersion(t, repo, "user_draft", 1)
	_, _, _, err := resolveProfileAndScenario(context.Background(), repo, repository.PlanParameters{
		AssumptionSelectionMode: SelectionPinnedProfile,
		ReturnAssumptionSetID:   "user_draft", ReturnAssumptionSetVersion: 1,
		ReturnAssumptionScenario: assumptions.ScenarioBaseline,
	})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != "assumption_profile_draft" {
		t.Fatalf("err=%v", err)
	}
}

func TestAssumptionActivationRollsBackWhenPreferenceMigrationFails(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)
	saveUserProfileVersion(t, repo, "user_rollback", 1)
	if _, err := repo.Activate(ctx, "user_rollback", 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetPreferences(ctx, repository.AssumptionPreferences{
		DefaultProfileID: "user_rollback", DefaultProfileVersion: 1,
		DefaultScenario: assumptions.ScenarioBaseline,
	}); err != nil {
		t.Fatal(err)
	}
	saveUserProfileVersion(t, repo, "user_rollback", 2)
	if _, err := db.Exec(`CREATE TRIGGER reject_assumption_preference_migration
		BEFORE UPDATE ON simulation_assumption_preferences
		BEGIN SELECT RAISE(ABORT, 'reject preference migration'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Activate(ctx, "user_rollback", 2); err == nil {
		t.Fatal("activation should fail")
	}
	v1, _ := repo.Get(ctx, "user_rollback", 1)
	v2, _ := repo.Get(ctx, "user_rollback", 2)
	pref, _ := repo.GetPreferences(ctx)
	if v1.Status != assumptions.StatusActive || v2.Status != assumptions.StatusDraft ||
		pref.DefaultProfileVersion != 1 {
		t.Fatalf("partial activation committed: v1=%s v2=%s pref=%+v", v1.Status, v2.Status, pref)
	}
}
