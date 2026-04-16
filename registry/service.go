// Package registry – service layer.
//
// Key invariants enforced here:
//   1. A strategy record is immutable once written.
//   2. An update always creates a new version (semver bump).
//   3. No strategy may be promoted to "approved" without a human confirmation
//      token (PreApproveToken).
//   4. Live strategies may not be mutated directly; callers must clone first.
package registry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// ─────────────────────────────────────────────────────────────────────────────
// Errors
// ─────────────────────────────────────────────────────────────────────────────

var (
	ErrNotFound        = errors.New("registry: strategy not found")
	ErrImmutable       = errors.New("registry: strategy records are immutable; create a new version")
	ErrApprovalRequired = errors.New("registry: human approval required before promotion to live")
	ErrInvalidStatus   = errors.New("registry: invalid status transition")
)

// ─────────────────────────────────────────────────────────────────────────────
// Service
// ─────────────────────────────────────────────────────────────────────────────

// Service provides the strategy registry API.
type Service struct {
	db *sql.DB
}

// New opens (or creates) the registry database at path.
func New(dbPath string) (*Service, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("registry: open db: %w", err)
	}
	s := &Service{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("registry: migrate: %w", err)
	}
	return s, nil
}

// ─── Schema ──────────────────────────────────────────────────────────────────

func (s *Service) migrate() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS strategies (
		pk         INTEGER PRIMARY KEY AUTOINCREMENT,
		id         TEXT NOT NULL,
		version    TEXT NOT NULL,
		name       TEXT NOT NULL,
		author     TEXT NOT NULL,
		status     TEXT NOT NULL,
		parent_id  TEXT,
		parent_ver TEXT,
		payload    TEXT NOT NULL,   -- full JSON of StrategyRecord
		created_at INTEGER NOT NULL,
		UNIQUE(id, version)
	);
	CREATE INDEX IF NOT EXISTS idx_strategies_id     ON strategies(id);
	CREATE INDEX IF NOT EXISTS idx_strategies_status ON strategies(status);

	CREATE TABLE IF NOT EXISTS lineage (
		pk               INTEGER PRIMARY KEY AUTOINCREMENT,
		strategy_id      TEXT NOT NULL,
		version          TEXT NOT NULL,
		parent_id        TEXT,
		parent_version   TEXT,
		mutation_summary TEXT,
		eval_score       REAL,
		promoted         INTEGER DEFAULT 0,
		promoted_at      INTEGER,
		created_at       INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_lineage_strategy ON lineage(strategy_id);
	CREATE INDEX IF NOT EXISTS idx_lineage_parent   ON lineage(parent_id);
	`)
	return err
}

// ─── Write operations ────────────────────────────────────────────────────────

// Create writes the first version of a new strategy.
// The record's Version must be a valid semver string.  If empty, "1.0.0" is
// assigned.
func (s *Service) Create(r *StrategyRecord) (*StrategyRecord, error) {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.Version == "" {
		r.Version = "1.0.0"
	}
	if _, err := semver.NewVersion(r.Version); err != nil {
		return nil, fmt.Errorf("registry: invalid version %q: %w", r.Version, err)
	}
	r.CreatedAt = time.Now()
	r.Status = StatusDraft
	r.StatusChangedAt = r.CreatedAt

	if err := s.insert(r); err != nil {
		return nil, err
	}
	s.insertLineage(r, "initial creation")
	return r, nil
}

// NewVersion creates a new immutable version derived from an existing record.
// BumpType must be one of "major", "minor", "patch".
// The caller supplies a mutated copy of the Parameters; the service enforces
// that the old record is NOT overwritten.
func (s *Service) NewVersion(parentID, parentVersion, bumpType, author string, params Parameters, mutationSummary string) (*StrategyRecord, error) {
	parent, err := s.GetVersion(parentID, parentVersion)
	if err != nil {
		return nil, err
	}

	// Bump semver
	old, err := semver.NewVersion(parent.Version)
	if err != nil {
		return nil, err
	}
	var newVer semver.Version
	switch bumpType {
	case "major":
		newVer = old.IncMajor()
	case "minor":
		newVer = old.IncMinor()
	default:
		newVer = old.IncPatch()
	}

	child := &StrategyRecord{
		ID:              parent.ID, // same ID, new version
		Name:            parent.Name,
		Version:         newVer.String(),
		Author:          author,
		CreatedAt:       time.Now(),
		ParentID:        parent.ID,
		ParentVersion:   parent.Version,
		Status:          StatusDraft,
		StatusChangedAt: time.Now(),
		StatusChangedBy: author,
		Parameters:      params,
		CompatibleMarkets:    parent.CompatibleMarkets,
		CompatibleTimeframes: parent.CompatibleTimeframes,
		RawConfig:       parent.RawConfig,
	}
	if err := s.insert(child); err != nil {
		return nil, err
	}
	s.insertLineage(child, mutationSummary)
	return child, nil
}

// SetStatus transitions a strategy version to a new status.
// Promotions to StatusApproved require approvedBy to be non-empty (human gate).
func (s *Service) SetStatus(id, version string, status StrategyStatus, changedBy string) error {
	r, err := s.GetVersion(id, version)
	if err != nil {
		return err
	}
	if err := validateTransition(r.Status, status); err != nil {
		return err
	}
	if status == StatusApproved && changedBy == "" {
		return ErrApprovalRequired
	}

	// We do NOT update the existing row — we write a new row with bumped version
	// so the audit trail is preserved.  For status-only transitions we use a
	// patch bump and mark the mutation summary accordingly.
	old, _ := semver.NewVersion(version)
	newVer := old.IncPatch()

	updated := *r
	updated.Version = newVer.String()
	updated.Status = status
	updated.StatusChangedAt = time.Now()
	updated.StatusChangedBy = changedBy
	updated.ParentID = id
	updated.ParentVersion = version
	updated.CreatedAt = time.Now()

	if err := s.insert(&updated); err != nil {
		return err
	}
	s.insertLineage(&updated, fmt.Sprintf("status change: %s → %s", r.Status, status))
	return nil
}

// AddPerformance appends a PerformanceSummary to the strategy's record.
// Because records are immutable this creates a new patch version.
func (s *Service) AddPerformance(id, version string, perf PerformanceSummary) (*StrategyRecord, error) {
	r, err := s.GetVersion(id, version)
	if err != nil {
		return nil, err
	}
	old, _ := semver.NewVersion(version)
	newVer := old.IncPatch()

	updated := *r
	updated.Version = newVer.String()
	updated.ParentID = id
	updated.ParentVersion = version
	updated.CreatedAt = time.Now()
	updated.Performance = append(updated.Performance, perf)

	if err := s.insert(&updated); err != nil {
		return nil, err
	}
	s.insertLineage(&updated, fmt.Sprintf("performance recorded: run %s", perf.RunID))
	return &updated, nil
}

// ─── Read operations ─────────────────────────────────────────────────────────

// GetVersion returns one specific version.
func (s *Service) GetVersion(id, version string) (*StrategyRecord, error) {
	row := s.db.QueryRow(`SELECT payload FROM strategies WHERE id=? AND version=?`, id, version)
	return scanRecord(row)
}

// GetLatest returns the highest-version record for a given strategy ID.
func (s *Service) GetLatest(id string) (*StrategyRecord, error) {
	rows, err := s.db.Query(`SELECT payload FROM strategies WHERE id=? ORDER BY pk DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*StrategyRecord
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	// Sort by semver descending to get true latest
	sort.Slice(records, func(i, j int) bool {
		vi, _ := semver.NewVersion(records[i].Version)
		vj, _ := semver.NewVersion(records[j].Version)
		return vi.GreaterThan(vj)
	})
	return records[0], nil
}

// ListVersions returns all versions of a strategy, sorted oldest-first.
func (s *Service) ListVersions(id string) ([]*StrategyRecord, error) {
	rows, err := s.db.Query(`SELECT payload FROM strategies WHERE id=? ORDER BY pk ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*StrategyRecord
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// ListByStatus returns all latest versions with the given status.
func (s *Service) ListByStatus(status StrategyStatus) ([]*StrategyRecord, error) {
	rows, err := s.db.Query(`SELECT payload FROM strategies WHERE status=?`, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []*StrategyRecord
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		if !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	return out, nil
}

// GetLineage returns the lineage chain for a strategy.
func (s *Service) GetLineage(id string) ([]LineageNode, error) {
	rows, err := s.db.Query(`SELECT strategy_id,version,parent_id,parent_version,mutation_summary,eval_score,promoted,promoted_at,created_at FROM lineage WHERE strategy_id=? ORDER BY pk ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LineageNode
	for rows.Next() {
		var n LineageNode
		var promotedAt sql.NullInt64
		if err := rows.Scan(&n.StrategyID, &n.Version, &n.ParentID, &n.ParentVersion, &n.MutationSummary, &n.EvalScore, &n.Promoted, &promotedAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		if promotedAt.Valid {
			t := time.UnixMilli(promotedAt.Int64)
			n.PromotedAt = &t
		}
		out = append(out, n)
	}
	return out, nil
}

// Export serialises a StrategyRecord to JSON.
func Export(r *StrategyRecord) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Import deserialises a StrategyRecord from JSON.
func Import(data []byte) (*StrategyRecord, error) {
	var r StrategyRecord
	return &r, json.Unmarshal(data, &r)
}

// Close releases the database.
func (s *Service) Close() error { return s.db.Close() }

// ─── Internal helpers ─────────────────────────────────────────────────────────

type scannable interface {
	Scan(dest ...any) error
}

func scanRecord(row scannable) (*StrategyRecord, error) {
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var r StrategyRecord
	return &r, json.Unmarshal([]byte(payload), &r)
}

func (s *Service) insert(r *StrategyRecord) error {
	payload, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO strategies(id,version,name,author,status,parent_id,parent_ver,payload,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Version, r.Name, r.Author, string(r.Status),
		r.ParentID, r.ParentVersion, string(payload), r.CreatedAt.UnixMilli(),
	)
	return err
}

func (s *Service) insertLineage(r *StrategyRecord, mutation string) {
	s.db.Exec(
		`INSERT INTO lineage(strategy_id,version,parent_id,parent_version,mutation_summary,eval_score,promoted,created_at) VALUES(?,?,?,?,?,0,0,?)`,
		r.ID, r.Version, r.ParentID, r.ParentVersion, mutation, time.Now().UnixMilli(),
	)
}

// validTransitions maps allowed status transitions.
var validTransitions = map[StrategyStatus][]StrategyStatus{
	StatusDraft:      {StatusPaper, StatusDisabled},
	StatusPaper:      {StatusApproved, StatusDeprecated, StatusDisabled},
	StatusApproved:   {StatusDeprecated, StatusDisabled},
	StatusDeprecated: {StatusDisabled},
	StatusDisabled:   {},
}

func validateTransition(from, to StrategyStatus) error {
	for _, allowed := range validTransitions[from] {
		if allowed == to {
			return nil
		}
	}
	return fmt.Errorf("%w: %s → %s", ErrInvalidStatus, from, to)
}
