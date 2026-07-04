package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fireman/fireman/internal/assumptions"
)

// ErrAssumptionProfileNotFound is returned when a profile id@version is missing.
var ErrAssumptionProfileNotFound = errors.New("assumption profile not found")

// ErrSystemProfileIdentityConflict is returned when a row that occupies a system
// identity (id+version) is not a recognized, immutable published content: e.g. an
// owner_scope=system row whose content hash matches no entry in the system content
// registry. The startup upgrade refuses to overwrite or silently accept it; a
// release data-repair script must resolve it explicitly (td/067 R13/R14).
var ErrSystemProfileIdentityConflict = errors.New("system profile identity conflict")

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

// EnsureSystemDefault performs the idempotent system-profile upgrade (td/064 R6,
// td/066 R12, td/067 R13/R14). It publishes the current system default
// (system_cma_v3@1) as a NEW immutable identity without ever updating or deleting
// the frozen system_cma_v1@1 / system_cma_v2@1, then atomically repoints the
// global default preference to v3 only when it is empty or still points at v3's
// DIRECT predecessor (v2). A preference pointing at a user-chosen custom profile,
// or at a non-direct predecessor (v1), is left untouched.
//
// Identity integrity (td/067 R13/R14, td/068 R16):
//   - The current identity row, if present, must be owner_scope=system and its
//     stored content hash + raw canonical bytes must equal the registry hash.
//   - A user profile that squats on the reserved system_cma_ namespace is migrated
//     to a deterministic user_legacy_<hash> id (repointing plan pins and the global
//     default) before the real system identity is published.
//   - EVERY surviving owner_scope=system reserved-namespace row (v1/v2/v3/future)
//     must match a recognized published content; an unknown or tampered system
//     content yields a conflict and is never overwritten.
//
// Safe to call on every startup/read path. The fast path is a read-only probe that
// returns immediately ONLY when there is no repair or audit work; otherwise all
// mutations and the full integrity audit happen inside one transaction.
func (r *AssumptionProfileRepo) EnsureSystemDefault(ctx context.Context) error {
	cur := assumptions.CurrentSystemIdentity()
	clean, err := r.systemNamespaceClean(ctx, cur)
	if err != nil {
		return err
	}
	if clean {
		return nil
	}
	// Slow path: fresh install, legacy (v1/v2) upgrade, a reserved-namespace
	// hijack to repair, or an unaudited system row. All work is transactional.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapSQL("begin system profile tx", err)
	}
	if err := r.runSystemDefaultUpgrade(ctx, tx, cur); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return wrapSQL("commit system profile", err)
	}
	return nil
}

// systemNamespaceClean reports whether the reserved system namespace needs no
// repair, publish or audit work, so EnsureSystemDefault can skip the upgrade
// transaction. It is clean ONLY when all three read-only probes pass (td/068 R16):
//   - the current identity (v3) exists, is owner_scope=system and matches the
//     registry canonical hash (stored content hash AND raw canonical bytes);
//   - no owner_scope=user profile squats on the reserved system_cma_ namespace;
//   - every owner_scope=system reserved-namespace row is registry-recognized and
//     untampered (its (id, version, content_hash) is registered AND its stored
//     bytes hash to content_hash).
//
// A present-but-tampered current identity surfaces a conflict immediately rather
// than deferring it to the transaction.
func (r *AssumptionProfileRepo) systemNamespaceClean(
	ctx context.Context, cur assumptions.SystemProfileIdentity,
) (bool, error) {
	row, found, err := probeProfileRow(ctx, r.db, cur.ID, cur.Version)
	if err != nil {
		return false, err
	}
	if !found || row.ownerScope != assumptions.OwnerSystem {
		return false, nil
	}
	if err := assertRecognizedCurrentIdentity(cur, row); err != nil {
		return false, err
	}
	hasSquatter, err := existsReservedUserProfile(ctx, r.db)
	if err != nil {
		return false, err
	}
	if hasSquatter {
		return false, nil
	}
	hasUnrecognized, err := existsUnrecognizedSystemRow(ctx, r.db)
	if err != nil {
		return false, err
	}
	return !hasUnrecognized, nil
}

// runSystemDefaultUpgrade executes the transactional upgrade/repair body.
func (r *AssumptionProfileRepo) runSystemDefaultUpgrade(
	ctx context.Context, tx *sql.Tx, cur assumptions.SystemProfileIdentity,
) error {
	// R13 #3: migrate any user profile squatting on the reserved system namespace
	// so the real system identity can be published without a primary-key clash.
	if err := repairReservedUserProfilesTx(ctx, tx); err != nil {
		return err
	}
	// R14/R16 #2: every surviving owner_scope=system reserved-namespace row
	// (v1/v2/v3/future) must be a recognized, untampered published content. This is
	// enforced uniformly at startup so it can never surface lazily at pin-run time.
	if err := auditReservedSystemRows(ctx, tx); err != nil {
		return err
	}
	// Re-evaluate the current identity inside the tx (a user v3 squatter is gone).
	row, found, err := probeProfileRow(ctx, tx, cur.ID, cur.Version)
	if err != nil {
		return err
	}
	if found {
		if row.ownerScope != assumptions.OwnerSystem {
			return fmt.Errorf("current system identity %s is owner_scope=%s: %w",
				cur.Ref(), row.ownerScope, ErrSystemProfileIdentityConflict)
		}
		// v3 already exists and was audited above. Do NOT migrate the default here:
		// only a freshly-published v3 triggers the v2->v3 repoint, so a user's
		// deliberate non-v3 default choice is preserved (td/068 R16 #3).
		return assertRecognizedCurrentIdentity(cur, row)
	}
	// Publish the current system identity and migrate the default from its direct
	// predecessor (v2 -> v3) only.
	p := assumptions.SystemDefaultProfile()
	if err := insertProfileTx(ctx, tx, p, assumptions.OwnerSystem, assumptions.StatusActive,
		assumptions.SystemProfileSourceNote, assumptions.SystemProfileReviewedBy,
		assumptions.SystemProfileReviewedAt); err != nil {
		return err
	}
	return r.migrateDefaultToCurrentSystem(ctx, tx)
}

// profileRow is the minimal identity-validation projection of a profile row.
type profileRow struct {
	ownerScope  string
	contentHash string
	canonical   string
}

// probeProfileRow loads the owner_scope, content hash and canonical JSON for an
// id@version, reporting found=false (no error) when the row is absent.
func probeProfileRow(ctx context.Context, q dbQuerier, id string, version int) (profileRow, bool, error) {
	var row profileRow
	err := q.QueryRowContext(ctx,
		`SELECT owner_scope, content_hash, canonical_json
		 FROM simulation_assumption_profiles WHERE id=? AND version=?`,
		id, version).Scan(&row.ownerScope, &row.contentHash, &row.canonical)
	if errors.Is(err, sql.ErrNoRows) {
		return profileRow{}, false, nil
	}
	if err != nil {
		return profileRow{}, false, wrapSQL("probe assumption profile row", err)
	}
	return row, true, nil
}

// assertRecognizedCurrentIdentity verifies a system-owned current-identity row is
// byte-faithful to the pinned registry canonical hash (td/067 R13 #2): the stored
// content hash AND the raw SHA-256 of the stored canonical JSON bytes must both
// equal the registry hash, otherwise the row was tampered/hijacked and is refused.
// The raw-byte hash is used (not a re-canonicalization of the decoded struct)
// because a legacy on-disk canonical can predate current struct fields and would
// drift when re-marshaled, yet its frozen byte hash is the immutable identity (see
// GetWithHash, td/067 R13/R14).
func assertRecognizedCurrentIdentity(cur assumptions.SystemProfileIdentity, row profileRow) error {
	if row.contentHash != cur.CanonicalHash {
		return fmt.Errorf("system identity %s stored content hash %q != registry %q: %w",
			cur.Ref(), row.contentHash, cur.CanonicalHash, ErrSystemProfileIdentityConflict)
	}
	if got := rawCanonicalHash(row.canonical); got != cur.CanonicalHash {
		return fmt.Errorf("system identity %s canonical bytes hash %q != registry %q: %w",
			cur.Ref(), got, cur.CanonicalHash, ErrSystemProfileIdentityConflict)
	}
	return nil
}

// reservedSystemRow is a reserved-namespace system profile row projected for the
// integrity audit.
type reservedSystemRow struct {
	id          string
	version     int
	contentHash string
	canonical   string
}

// auditReservedSystemRows validates EVERY owner_scope=system row in the reserved
// system_cma_ namespace (td/068 R16 #2). Each row's (id, version, content_hash)
// must be a recognized published content AND its stored canonical_json bytes must
// hash (raw SHA-256) to that content_hash. Any unknown or tampered row yields a
// conflict and is never overwritten, so the integrity invariant is enforced
// uniformly at startup instead of lazily when the row is later pinned into a run.
func auditReservedSystemRows(ctx context.Context, q dbRowsQuerier) error {
	rows, err := listReservedSystemRows(ctx, q)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, ok := assumptions.LookupSystemContent(row.id, row.version, row.contentHash); !ok {
			return fmt.Errorf("system row %s@%d content hash %q is not a recognized published content: %w",
				row.id, row.version, row.contentHash, ErrSystemProfileIdentityConflict)
		}
		if got := rawCanonicalHash(row.canonical); got != row.contentHash {
			return fmt.Errorf("system row %s@%d canonical bytes hash %q != content_hash %q: %w",
				row.id, row.version, got, row.contentHash, ErrSystemProfileIdentityConflict)
		}
	}
	return nil
}

// reservedSystemRowRecognized reports whether a reserved system row is a
// recognized, untampered published content: its (id, version, content_hash) is in
// the registry AND its stored canonical bytes hash to that content_hash.
func reservedSystemRowRecognized(row reservedSystemRow) bool {
	if _, ok := assumptions.LookupSystemContent(row.id, row.version, row.contentHash); !ok {
		return false
	}
	return rawCanonicalHash(row.canonical) == row.contentHash
}

// listReservedSystemRows returns every owner_scope=system row whose id is in the
// reserved system_cma_ namespace, on either the connection or the upgrade tx.
func listReservedSystemRows(ctx context.Context, q dbRowsQuerier) ([]reservedSystemRow, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, version, content_hash, canonical_json
		 FROM simulation_assumption_profiles
		 WHERE owner_scope=? AND id LIKE 'system\_cma\_%' ESCAPE '\'`,
		assumptions.OwnerSystem)
	if err != nil {
		return nil, wrapSQL("list reserved-namespace system rows", err)
	}
	defer func() { _ = rows.Close() }()
	var out []reservedSystemRow
	for rows.Next() {
		var row reservedSystemRow
		if err := rows.Scan(&row.id, &row.version, &row.contentHash, &row.canonical); err != nil {
			return nil, wrapSQL("scan reserved-namespace system row", err)
		}
		out = append(out, row)
	}
	return out, wrapSQL("iterate reserved-namespace system rows", rows.Err())
}

// existsReservedUserProfile reports whether any owner_scope=user profile squats on
// the reserved system_cma_ namespace and must be migrated (td/068 R16 #1).
func existsReservedUserProfile(ctx context.Context, db *sql.DB) (bool, error) {
	var present int
	err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM simulation_assumption_profiles
		   WHERE owner_scope=? AND id LIKE 'system\_cma\_%' ESCAPE '\')`,
		assumptions.OwnerUser).Scan(&present)
	if err != nil {
		return false, wrapSQL("probe reserved-namespace user profiles", err)
	}
	return present != 0, nil
}

// existsUnrecognizedSystemRow reports whether any owner_scope=system reserved row
// fails the integrity check (unknown (id, version, content_hash) OR stored bytes
// that no longer hash to content_hash). It is the read-only fast-path signal that
// mirrors the transactional auditReservedSystemRows, so the integrity invariant is
// validated on every startup/read, not only when an upgrade transaction runs
// (td/068 R16 #1).
func existsUnrecognizedSystemRow(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := listReservedSystemRows(ctx, db)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if !reservedSystemRowRecognized(row) {
			return true, nil
		}
	}
	return false, nil
}

// rawCanonicalHash is the SHA-256 of the stored canonical JSON bytes (the frozen
// content_hash notion), independent of any current-struct re-canonicalization.
func rawCanonicalHash(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// legacySquatter is one user-owned profile that illegally occupies the reserved
// system_cma_ namespace and must be migrated to a deterministic user_legacy_ id.
type legacySquatter struct {
	oldID, newID           string
	version                int
	name, status           string
	canonical, sourceNote  string
	reviewedBy, reviewedAt string
}

// repairReservedUserProfilesTx migrates every owner_scope=user profile whose id is
// in the reserved system_cma_ namespace to a deterministic user_legacy_<old hash
// prefix> id, repointing plan pins and the global default to the new id before
// deleting the conflicting row (td/067 R13 #3). Frozen run input snapshots are
// never rewritten.
func repairReservedUserProfilesTx(ctx context.Context, tx *sql.Tx) error {
	squatters, err := collectReservedUserProfiles(ctx, tx)
	if err != nil {
		return err
	}
	for _, sq := range squatters {
		if err := migrateLegacySquatter(ctx, tx, sq); err != nil {
			return err
		}
	}
	return nil
}

func collectReservedUserProfiles(ctx context.Context, tx *sql.Tx) ([]legacySquatter, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, version, name, status, canonical_json, content_hash,
		        source_note, reviewed_by, reviewed_at
		 FROM simulation_assumption_profiles
		 WHERE owner_scope=? AND id LIKE 'system\_cma\_%' ESCAPE '\'`,
		assumptions.OwnerUser)
	if err != nil {
		return nil, wrapSQL("list reserved-namespace user profiles", err)
	}
	defer func() { _ = rows.Close() }()
	var out []legacySquatter
	for rows.Next() {
		var sq legacySquatter
		var contentHash string
		if err := rows.Scan(&sq.oldID, &sq.version, &sq.name, &sq.status, &sq.canonical,
			&contentHash, &sq.sourceNote, &sq.reviewedBy, &sq.reviewedAt); err != nil {
			return nil, wrapSQL("scan reserved-namespace user profile", err)
		}
		sq.newID = legacyUserProfileID(contentHash)
		out = append(out, sq)
	}
	return out, wrapSQL("iterate reserved-namespace user profiles", rows.Err())
}

// legacyUserProfileID derives the deterministic migration id from the old
// canonical content hash (td/067 R13 #3).
func legacyUserProfileID(oldContentHash string) string {
	h := oldContentHash
	if len(h) > 16 {
		h = h[:16]
	}
	return "user_legacy_" + h
}

func migrateLegacySquatter(ctx context.Context, tx *sql.Tx, sq legacySquatter) error {
	var p assumptions.Profile
	if err := json.Unmarshal([]byte(sq.canonical), &p); err != nil {
		return fmt.Errorf("decode squatter %s@%d canonical: %w", sq.oldID, sq.version, err)
	}
	p.ID = sq.newID
	if err := insertProfileTx(ctx, tx, p, assumptions.OwnerUser, sq.status,
		sq.sourceNote, sq.reviewedBy, sq.reviewedAt); err != nil {
		return fmt.Errorf("insert migrated user profile %s@%d: %w", sq.newID, sq.version, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE plan_parameters SET return_assumption_set_id=?
		 WHERE return_assumption_set_id=? AND return_assumption_set_version=?`,
		sq.newID, sq.oldID, sq.version); err != nil {
		return wrapSQL("repoint plan pinned assumption profile", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE simulation_assumption_preferences SET default_profile_id=?, updated_at=?
		 WHERE id=1 AND default_profile_id=? AND default_profile_version=?`,
		sq.newID, time.Now().UnixMilli(), sq.oldID, sq.version); err != nil {
		return wrapSQL("repoint global default assumption preference", err)
	}
	return deleteProfileVersionTx(ctx, tx, sq.oldID, sq.version)
}

// deleteProfileVersionTx removes a single profile version and its normalized
// projections (explicitly, so it works regardless of the SQLite foreign-key
// pragma).
func deleteProfileVersionTx(ctx context.Context, tx *sql.Tx, id string, version int) error {
	for _, stmt := range []string{
		`DELETE FROM simulation_assumption_scenarios WHERE profile_id=? AND profile_version=?`,
		`DELETE FROM simulation_assumption_return_priors WHERE profile_id=? AND profile_version=?`,
		`DELETE FROM simulation_assumption_correlation_priors WHERE profile_id=? AND profile_version=?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id, version); err != nil {
			return wrapSQL("delete assumption projection", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM simulation_assumption_profiles WHERE id=? AND version=?`, id, version); err != nil {
		return wrapSQL("delete conflicting assumption profile", err)
	}
	return nil
}

// migrateDefaultToCurrentSystem atomically repoints the single global default
// preference from the current identity's DIRECT predecessor (system_cma_v2@1) to
// the current system default (system_cma_v3@1 / baseline) ONLY when it currently
// points at that direct predecessor. A preference row pointing at a user-chosen
// custom profile, or at a non-direct predecessor (system_cma_v1@1), is left
// untouched; a missing preference row resolves to the current default via
// GetPreferences's fallback (td/064 R6 / td/066 R12).
func (r *AssumptionProfileRepo) migrateDefaultToCurrentSystem(ctx context.Context, tx *sql.Tx) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx,
		`UPDATE simulation_assumption_preferences
		 SET default_profile_id=?, default_profile_version=?, default_scenario=?, updated_at=?
		 WHERE id=1 AND default_profile_id=? AND default_profile_version=?`,
		assumptions.SystemProfileID, assumptions.SystemProfileVersion,
		assumptions.ScenarioBaseline, time.Now().UnixMilli(),
		assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version)
	return wrapSQL("migrate default assumption preference", err)
}

func (r *AssumptionProfileRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

// dbQuerier is satisfied by both *sql.DB and *sql.Tx, letting identity probes run
// on either the connection (fast path) or inside the upgrade transaction.
type dbQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// dbRowsQuerier is satisfied by both *sql.DB and *sql.Tx, letting the reserved-row
// audit enumerate on either the connection (fast path) or the upgrade transaction.
type dbRowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
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

// GetWithHash is Get plus the FROZEN stored content_hash column. The stored hash
// is the canonical identity of the row even when the on-disk canonical JSON
// predates current struct fields (so re-canonicalizing the decoded struct would
// drift); run provenance must use it (td/067 R13/R14).
func (r *AssumptionProfileRepo) GetWithHash(
	ctx context.Context, id string, version int,
) (assumptions.Profile, string, error) {
	var canonical, status, hash string
	err := r.db.QueryRowContext(ctx,
		`SELECT canonical_json, status, content_hash FROM simulation_assumption_profiles
		 WHERE id=? AND version=?`, id, version).Scan(&canonical, &status, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return assumptions.Profile{}, "", ErrAssumptionProfileNotFound
	}
	if err != nil {
		return assumptions.Profile{}, "", wrapSQL("get assumption profile", err)
	}
	var p assumptions.Profile
	if err := json.Unmarshal([]byte(canonical), &p); err != nil {
		return assumptions.Profile{}, "", wrapSQL("decode assumption profile", err)
	}
	p.Status = status
	return p, hash, nil
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
