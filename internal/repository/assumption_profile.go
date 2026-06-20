package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fireman/fireman/internal/assumptions"
)

// ErrAssumptionProfileNotFound is returned when a profile id@version is missing.
var ErrAssumptionProfileNotFound = errors.New("assumption profile not found")

// AssumptionProfileRepo persists global simulation assumption profiles, their
// normalized projections and the single-row user preference (td/061 §4.1).
type AssumptionProfileRepo struct {
	db *sql.DB
}

func NewAssumptionProfileRepo(db *sql.DB) *AssumptionProfileRepo {
	return &AssumptionProfileRepo{db: db}
}

// ProfileSummary is a list-row projection (without the full canonical JSON).
type ProfileSummary struct {
	ID          string `json:"id"`
	Version     int    `json:"version"`
	OwnerScope  string `json:"owner_scope"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	ContentHash string `json:"content_hash"`
	SourceNote  string `json:"source_note,omitempty"`
	ReviewedBy  string `json:"reviewed_by,omitempty"`
	ReviewedAt  string `json:"reviewed_at,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	// EligibleForGlobalDefault reports whether this profile may be selected as the
	// user's global default: it must be active AND still pass the current publish
	// gate (structure + coverage + PSD + tail). The legacy system_cma_v1@1 stays
	// active for replay/pins but is NOT eligible (td/065 R8). Computed by the
	// service, not stored.
	EligibleForGlobalDefault bool `json:"eligible_for_global_default"`
}

// AssumptionPreferences is the resolved global default selection.
type AssumptionPreferences struct {
	DefaultProfileID      string `json:"default_profile_id"`
	DefaultProfileVersion int    `json:"default_profile_version"`
	DefaultScenario       string `json:"default_scenario"`
}

// EnsureSystemDefault performs the idempotent system-profile upgrade (td/064 R6).
// It publishes the current system default (system_cma_v2@1) as a NEW immutable
// identity without ever updating or deleting the legacy system_cma_v1@1, then
// atomically repoints the global default preference from v1 to v2 only when it
// still points at v1 (user custom defaults and explicit pins are untouched).
// Safe to call on every startup and on every read path.
func (r *AssumptionProfileRepo) EnsureSystemDefault(ctx context.Context) error {
	p := assumptions.SystemDefaultProfile()
	var exists int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM simulation_assumption_profiles WHERE id=? AND version=?`,
		p.ID, p.Version).Scan(&exists)
	if err != nil {
		return wrapSQL("probe system assumption profile", err)
	}
	if exists > 0 {
		// Already upgraded. The one-time v1->v2 default migration ran inside the
		// upgrade transaction below, so there is nothing to do on subsequent calls
		// (and re-running it would fight a user who deliberately re-selects v1).
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapSQL("begin system profile tx", err)
	}
	if err := insertProfileTx(ctx, tx, p, assumptions.OwnerSystem, assumptions.StatusActive,
		assumptions.SystemProfileSourceNote, assumptions.SystemProfileReviewedBy,
		assumptions.SystemProfileReviewedAt); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := r.migrateDefaultToCurrentSystem(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return wrapSQL("commit system profile", err)
	}
	return nil
}

// migrateDefaultToCurrentSystem atomically repoints the single global default
// preference from the legacy system_cma_v1@1 to the current system default
// (system_cma_v2@1 / baseline) ONLY when it currently points at the legacy
// identity. A preference row pointing at a user-chosen custom profile is left
// untouched, and a missing preference row resolves to the current default via
// GetPreferences's fallback (td/064 R6).
func (r *AssumptionProfileRepo) migrateDefaultToCurrentSystem(ctx context.Context, tx *sql.Tx) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx,
		`UPDATE simulation_assumption_preferences
		 SET default_profile_id=?, default_profile_version=?, default_scenario=?, updated_at=?
		 WHERE id=1 AND default_profile_id=? AND default_profile_version=?`,
		assumptions.SystemProfileID, assumptions.SystemProfileVersion,
		assumptions.ScenarioBaseline, time.Now().UnixMilli(),
		assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion)
	return wrapSQL("migrate default assumption preference", err)
}

func (r *AssumptionProfileRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

// Get returns a single profile id@version decoded from its canonical JSON. The
// lifecycle status comes from the row (not the frozen JSON) so a profile that was
// activated/superseded after it was first saved reports its current status.
func (r *AssumptionProfileRepo) Get(ctx context.Context, id string, version int) (assumptions.Profile, error) {
	var canonical, status string
	err := r.db.QueryRowContext(ctx,
		`SELECT canonical_json, status FROM simulation_assumption_profiles WHERE id=? AND version=?`,
		id, version).Scan(&canonical, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return assumptions.Profile{}, ErrAssumptionProfileNotFound
	}
	if err != nil {
		return assumptions.Profile{}, wrapSQL("get assumption profile", err)
	}
	var p assumptions.Profile
	if err := json.Unmarshal([]byte(canonical), &p); err != nil {
		return assumptions.Profile{}, wrapSQL("decode assumption profile", err)
	}
	p.Status = status
	return p, nil
}

// GetActiveLatest returns the highest active version for an id.
func (r *AssumptionProfileRepo) GetActiveLatest(ctx context.Context, id string) (assumptions.Profile, error) {
	var canonical string
	err := r.db.QueryRowContext(ctx,
		`SELECT canonical_json FROM simulation_assumption_profiles
		 WHERE id=? AND status=? ORDER BY version DESC LIMIT 1`,
		id, assumptions.StatusActive).Scan(&canonical)
	if errors.Is(err, sql.ErrNoRows) {
		return assumptions.Profile{}, ErrAssumptionProfileNotFound
	}
	if err != nil {
		return assumptions.Profile{}, wrapSQL("get active assumption profile", err)
	}
	var p assumptions.Profile
	if err := json.Unmarshal([]byte(canonical), &p); err != nil {
		return assumptions.Profile{}, wrapSQL("decode assumption profile", err)
	}
	return p, nil
}

// List returns all profile summaries ordered by id then version.
func (r *AssumptionProfileRepo) List(ctx context.Context) ([]ProfileSummary, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, version, owner_scope, name, status, content_hash,
		        source_note, reviewed_by, reviewed_at, created_at, updated_at
		 FROM simulation_assumption_profiles ORDER BY id, version`)
	if err != nil {
		return nil, wrapSQL("list assumption profiles", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ProfileSummary
	for rows.Next() {
		var s ProfileSummary
		if err := rows.Scan(&s.ID, &s.Version, &s.OwnerScope, &s.Name, &s.Status,
			&s.ContentHash, &s.SourceNote, &s.ReviewedBy, &s.ReviewedAt,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, wrapSQL("scan assumption profile summary", err)
		}
		out = append(out, s)
	}
	return out, wrapSQL("iterate assumption profiles", rows.Err())
}

// Save persists a (draft) profile version with its projections in one tx. The
// (id, version) must not already exist; active versions are never updated.
func (r *AssumptionProfileRepo) Save(
	ctx context.Context, p assumptions.Profile, sourceNote, reviewedBy, reviewedAt string,
) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("validate assumption profile: %w", err)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapSQL("begin save profile tx", err)
	}
	if err := insertProfileTx(ctx, tx, p, p.OwnerScope, p.Status, sourceNote, reviewedBy, reviewedAt); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return wrapSQL("commit save profile", err)
	}
	return nil
}

// Activate marks a draft version active and supersedes other active versions of
// the same id. Active rows are otherwise immutable.
func (r *AssumptionProfileRepo) Activate(ctx context.Context, id string, version int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapSQL("begin activate profile tx", err)
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx,
		`UPDATE simulation_assumption_profiles SET status=?, updated_at=?
		 WHERE id=? AND status=?`,
		assumptions.StatusSuperseded, now, id, assumptions.StatusActive); err != nil {
		_ = tx.Rollback()
		return wrapSQL("supersede active profile", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE simulation_assumption_profiles SET status=?, updated_at=?
		 WHERE id=? AND version=? AND status=?`,
		assumptions.StatusActive, now, id, version, assumptions.StatusDraft)
	if err != nil {
		_ = tx.Rollback()
		return wrapSQL("activate profile version", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = tx.Rollback()
		return ErrAssumptionProfileNotFound
	}
	if err := tx.Commit(); err != nil {
		return wrapSQL("commit activate profile", err)
	}
	return nil
}

// GetPreferences returns the user's global default selection, falling back to the
// system profile/baseline when no row exists yet.
func (r *AssumptionProfileRepo) GetPreferences(ctx context.Context) (AssumptionPreferences, error) {
	var pref AssumptionPreferences
	err := r.db.QueryRowContext(ctx,
		`SELECT default_profile_id, default_profile_version, default_scenario
		 FROM simulation_assumption_preferences WHERE id=1`).Scan(
		&pref.DefaultProfileID, &pref.DefaultProfileVersion, &pref.DefaultScenario)
	if errors.Is(err, sql.ErrNoRows) {
		return AssumptionPreferences{
			DefaultProfileID:      assumptions.SystemProfileID,
			DefaultProfileVersion: assumptions.SystemProfileVersion,
			DefaultScenario:       assumptions.ScenarioBaseline,
		}, nil
	}
	if err != nil {
		return AssumptionPreferences{}, wrapSQL("get assumption preferences", err)
	}
	if pref.DefaultProfileID == "" {
		pref.DefaultProfileID = assumptions.SystemProfileID
		pref.DefaultProfileVersion = assumptions.SystemProfileVersion
	}
	if pref.DefaultScenario == "" {
		pref.DefaultScenario = assumptions.ScenarioBaseline
	}
	return pref, nil
}

// SetPreferences upserts the single preference row.
func (r *AssumptionProfileRepo) SetPreferences(ctx context.Context, pref AssumptionPreferences) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO simulation_assumption_preferences
		   (id, default_profile_id, default_profile_version, default_scenario, updated_at)
		 VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   default_profile_id=excluded.default_profile_id,
		   default_profile_version=excluded.default_profile_version,
		   default_scenario=excluded.default_scenario,
		   updated_at=excluded.updated_at`,
		pref.DefaultProfileID, pref.DefaultProfileVersion, pref.DefaultScenario, time.Now().UnixMilli())
	return wrapSQL("set assumption preferences", err)
}

func insertProfileTx(
	ctx context.Context, tx *sql.Tx, p assumptions.Profile,
	ownerScope, status, sourceNote, reviewedBy, reviewedAt string,
) error {
	canonical, err := p.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("canonical json: %w", err)
	}
	hash, err := p.ContentHash()
	if err != nil {
		return fmt.Errorf("content hash: %w", err)
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO simulation_assumption_profiles
		   (id, version, owner_scope, name, status, canonical_json, content_hash,
		    source_note, reviewed_by, reviewed_at, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Version, ownerScope, p.Name, status, string(canonical), hash,
		sourceNote, reviewedBy, reviewedAt, now, now); err != nil {
		return wrapSQL("insert assumption profile", err)
	}
	return insertProfileProjectionsTx(ctx, tx, p)
}

func insertProfileProjectionsTx(ctx context.Context, tx *sql.Tx, p assumptions.Profile) error {
	for name, sc := range p.Scenarios {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO simulation_assumption_scenarios
			   (profile_id, profile_version, scenario, return_shift_log, return_shift_log_fx, volatility_multiplier)
			 VALUES (?,?,?,?,?,?)`,
			p.ID, p.Version, name, sc.ReturnShiftLog, sc.ReturnShiftLogFX, sc.VolatilityMultiplier); err != nil {
			return wrapSQL("insert assumption scenario projection", err)
		}
	}
	for _, rp := range p.ReturnPriors {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO simulation_assumption_return_priors
			   (profile_id, profile_version, asset_class, region, valuation_currency,
			    annual_geometric_return, annual_volatility_floor, annual_volatility_ceiling,
			    source_url, published_at, reviewed_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, p.Version, rp.AssetClass, rp.Region, rp.ValuationCurrency,
			rp.AnnualGeometricReturn, rp.AnnualVolatilityFloor, rp.AnnualVolatilityCeiling,
			rp.SourceURL, rp.PublishedAt, rp.ReviewedAt); err != nil {
			return wrapSQL("insert assumption return prior projection", err)
		}
	}
	for _, c := range p.CorrelationPriors {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO simulation_assumption_correlation_priors
			   (profile_id, profile_version, factor_a, factor_b, rho)
			 VALUES (?,?,?,?,?)`,
			p.ID, p.Version, c.FactorA, c.FactorB, c.Rho); err != nil {
			return wrapSQL("insert assumption correlation prior projection", err)
		}
	}
	return nil
}
