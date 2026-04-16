// Package journal – service layer.
//
// DecisionEntry rows are append-only.  Outcome updates are the only
// mutation allowed, and they only fill in the outcome sub-object.
// Compaction is additive: a StrategySummary row is written; individual rows
// are not deleted (they are archived instead).
package journal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// ErrNotFound is returned when a decision is not in the journal.
var ErrNotFound = errors.New("journal: decision not found")

// ─────────────────────────────────────────────────────────────────────────────
// Service
// ─────────────────────────────────────────────────────────────────────────────

// Service provides the decision journal API.
type Service struct {
	db *sql.DB
}

// New opens (or creates) the journal database at path.
func New(dbPath string) (*Service, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("journal: open db: %w", err)
	}
	s := &Service{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("journal: migrate: %w", err)
	}
	return s, nil
}

// ─── Schema ──────────────────────────────────────────────────────────────────

func (s *Service) migrate() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS decisions (
		pk               INTEGER PRIMARY KEY AUTOINCREMENT,
		decision_id      TEXT UNIQUE NOT NULL,
		strategy_id      TEXT NOT NULL,
		strategy_version TEXT NOT NULL,
		session_id       TEXT NOT NULL,
		cycle_number     INTEGER NOT NULL,
		symbol           TEXT NOT NULL,
		ts               INTEGER NOT NULL,
		mode             TEXT NOT NULL,
		action           TEXT NOT NULL,
		confidence       INTEGER NOT NULL DEFAULT 0,
		outcome_class    TEXT NOT NULL DEFAULT 'pending',
		archived         INTEGER NOT NULL DEFAULT 0,
		payload          TEXT NOT NULL   -- full JSON of DecisionEntry
	);
	CREATE INDEX IF NOT EXISTS idx_d_strategy   ON decisions(strategy_id, strategy_version);
	CREATE INDEX IF NOT EXISTS idx_d_symbol     ON decisions(symbol);
	CREATE INDEX IF NOT EXISTS idx_d_ts         ON decisions(ts);
	CREATE INDEX IF NOT EXISTS idx_d_outcome    ON decisions(outcome_class);
	CREATE INDEX IF NOT EXISTS idx_d_session    ON decisions(session_id);

	CREATE TABLE IF NOT EXISTS summaries (
		pk               INTEGER PRIMARY KEY AUTOINCREMENT,
		summary_id       TEXT UNIQUE NOT NULL,
		strategy_id      TEXT NOT NULL,
		strategy_version TEXT NOT NULL,
		symbol           TEXT,
		from_ts          INTEGER NOT NULL,
		to_ts            INTEGER NOT NULL,
		payload          TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_s_strategy ON summaries(strategy_id, strategy_version);
	`)
	return err
}

// ─── Write ────────────────────────────────────────────────────────────────────

// Record writes a new decision entry.
func (s *Service) Record(e *DecisionEntry) error {
	if e.DecisionID == "" {
		e.DecisionID = uuid.NewString()
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO decisions(decision_id,strategy_id,strategy_version,session_id,cycle_number,symbol,ts,mode,action,confidence,payload)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		e.DecisionID, e.StrategyID, e.StrategyVersion, e.SessionID, e.CycleNumber,
		e.Symbol, e.Timestamp.UnixMilli(), string(e.Mode), string(e.Action), e.Confidence, string(payload),
	)
	return err
}

// RecordOutcome fills in the Outcome for an existing decision.
// This is the ONLY mutation allowed after initial insertion.
func (s *Service) RecordOutcome(decisionID string, outcome Outcome) error {
	e, err := s.Get(decisionID)
	if err != nil {
		return err
	}
	e.Outcome = &outcome

	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE decisions SET outcome_class=?, payload=? WHERE decision_id=?`,
		string(outcome.Class), string(payload), decisionID,
	)
	return err
}

// AddReviewNote appends a human review note to a decision.
func (s *Service) AddReviewNote(decisionID, note, reviewer string) error {
	e, err := s.Get(decisionID)
	if err != nil {
		return err
	}
	e.ReviewNotes = note
	now := time.Now()
	e.ReviewedAt = &now
	e.ReviewedBy = reviewer

	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE decisions SET payload=? WHERE decision_id=?`, string(payload), decisionID)
	return err
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// Get returns one decision by ID.
func (s *Service) Get(decisionID string) (*DecisionEntry, error) {
	row := s.db.QueryRow(`SELECT payload FROM decisions WHERE decision_id=?`, decisionID)
	return scanDecision(row)
}

// Query returns decisions matching the filter.
func (s *Service) Query(f QueryFilter) ([]*DecisionEntry, error) {
	where, args := buildWhere(f)
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	q := fmt.Sprintf(`SELECT payload FROM decisions WHERE %s ORDER BY ts DESC LIMIT ? OFFSET ?`, where)
	args = append(args, limit, f.Offset)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DecisionEntry
	for rows.Next() {
		e, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LatestForSymbol returns the most recent decision for a strategy+symbol pair.
// Used by the evaluator to provide prior-decision context to the AI.
func (s *Service) LatestForSymbol(strategyID, version, symbol string, n int) ([]*DecisionEntry, error) {
	if n <= 0 {
		n = 5
	}
	rows, err := s.db.Query(
		`SELECT payload FROM decisions WHERE strategy_id=? AND strategy_version=? AND symbol=? ORDER BY ts DESC LIMIT ?`,
		strategyID, version, symbol, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DecisionEntry
	for rows.Next() {
		e, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─── Compaction ──────────────────────────────────────────────────────────────

// Compact generates a StrategySummary for all decisions of a strategy version
// older than retainDays.  Compacted rows are marked archived=1 but not
// deleted (they can still be retrieved with includeArchived=true).
func (s *Service) Compact(strategyID, version string, retainDays int) (*StrategySummary, error) {
	cutoff := time.Now().AddDate(0, 0, -retainDays).UnixMilli()

	rows, err := s.db.Query(
		`SELECT payload FROM decisions WHERE strategy_id=? AND strategy_version=? AND ts < ? AND archived=0`,
		strategyID, version, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*DecisionEntry
	for rows.Next() {
		e, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, nil
	}

	summary := buildSummary(strategyID, version, "", entries)

	// Write summary
	payload, _ := json.Marshal(summary)
	sid := uuid.NewString()
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO summaries(summary_id,strategy_id,strategy_version,symbol,from_ts,to_ts,payload) VALUES(?,?,?,?,?,?,?)`,
		sid, strategyID, version, "", summary.From.UnixMilli(), summary.To.UnixMilli(), string(payload),
	)
	if err != nil {
		return nil, err
	}

	// Mark compacted rows as archived
	_, err = s.db.Exec(
		`UPDATE decisions SET archived=1 WHERE strategy_id=? AND strategy_version=? AND ts < ?`,
		strategyID, version, cutoff,
	)
	return summary, err
}

// GetSummary returns the latest compacted summary for a strategy version.
func (s *Service) GetSummary(strategyID, version string) (*StrategySummary, error) {
	row := s.db.QueryRow(
		`SELECT payload FROM summaries WHERE strategy_id=? AND strategy_version=? ORDER BY pk DESC LIMIT 1`,
		strategyID, version,
	)
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var out StrategySummary
	return &out, json.Unmarshal([]byte(payload), &out)
}

// Close releases the database.
func (s *Service) Close() error { return s.db.Close() }

// ─── Internal helpers ─────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDecision(row rowScanner) (*DecisionEntry, error) {
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var e DecisionEntry
	return &e, json.Unmarshal([]byte(payload), &e)
}

func buildWhere(f QueryFilter) (string, []any) {
	var clauses []string
	var args []any
	clauses = append(clauses, "archived=0")

	if f.StrategyID != "" {
		clauses = append(clauses, "strategy_id=?")
		args = append(args, f.StrategyID)
	}
	if f.StrategyVersion != "" {
		clauses = append(clauses, "strategy_version=?")
		args = append(args, f.StrategyVersion)
	}
	if f.Symbol != "" {
		clauses = append(clauses, "symbol=?")
		args = append(args, f.Symbol)
	}
	if f.Mode != "" {
		clauses = append(clauses, "mode=?")
		args = append(args, string(f.Mode))
	}
	if f.From != nil {
		clauses = append(clauses, "ts >= ?")
		args = append(args, f.From.UnixMilli())
	}
	if f.To != nil {
		clauses = append(clauses, "ts <= ?")
		args = append(args, f.To.UnixMilli())
	}
	if f.OutcomeClass != "" {
		clauses = append(clauses, "outcome_class=?")
		args = append(args, string(f.OutcomeClass))
	}
	return strings.Join(clauses, " AND "), args
}

func buildSummary(strategyID, version, symbol string, entries []*DecisionEntry) StrategySummary {
	s := StrategySummary{
		StrategyID:      strategyID,
		StrategyVersion: version,
		Symbol:          symbol,
		TotalDecisions:  len(entries),
	}
	if len(entries) == 0 {
		return s
	}

	minTs := entries[0].Timestamp
	maxTs := entries[0].Timestamp
	totalReturn := 0.0
	totalPnL := 0.0
	equity := 0.0
	peak := 0.0
	maxDD := 0.0

	for _, e := range entries {
		if e.Timestamp.Before(minTs) {
			minTs = e.Timestamp
		}
		if e.Timestamp.After(maxTs) {
			maxTs = e.Timestamp
		}
		if e.Outcome != nil {
			switch e.Outcome.Class {
			case OutcomeWin:
				s.Wins++
			case OutcomeLoss:
				s.Losses++
			}
			totalReturn += e.Outcome.ReturnPct
			totalPnL += e.Outcome.RealizedPnL
			equity += e.Outcome.RealizedPnL
			if equity > peak {
				peak = equity
			}
			dd := (peak - equity) / math.Max(peak, 1)
			if dd > maxDD {
				maxDD = dd
			}
		}
	}

	s.From = minTs
	s.To = maxTs
	s.TotalPnL = totalPnL
	closed := s.Wins + s.Losses
	if closed > 0 {
		s.WinRate = float64(s.Wins) / float64(closed)
		s.AvgReturnPct = totalReturn / float64(closed)
	}
	s.MaxDrawdown = -maxDD

	// Keep last 10 as brief decisions
	n := 10
	if len(entries) < n {
		n = len(entries)
	}
	for _, e := range entries[len(entries)-n:] {
		bd := BriefDecision{
			Timestamp:  e.Timestamp,
			Symbol:     e.Symbol,
			Action:     e.Action,
			Confidence: e.Confidence,
			Result:     OutcomePending,
		}
		if e.Outcome != nil {
			bd.Result = e.Outcome.Class
			bd.ReturnPct = e.Outcome.ReturnPct
		}
		s.RecentDecisions = append(s.RecentDecisions, bd)
	}
	return s
}
