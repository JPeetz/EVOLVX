// Package optimizer – job orchestrator.
//
// Service ties together the candidate generator, evaluator, and promoter into
// a single run() method.  It persists job state to SQLite so jobs survive
// restarts.  Evaluation runs are parallelised with a configurable worker pool.
package optimizer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/NoFxAiOS/nofx/registry"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// ─────────────────────────────────────────────────────────────────────────────
// Service
// ─────────────────────────────────────────────────────────────────────────────

// Service orchestrates optimization jobs.
type Service struct {
	db        *sql.DB
	reg       *registry.Service
	runner    BacktestRunner
	promoter  *Promoter
	workers   int
}

// New creates an optimizer service.
//
//   dbPath:  path for optimizer state (jobs, candidates)
//   reg:     strategy registry
//   runner:  function that runs a backtest for given params
//   workers: parallel evaluation workers (0 = 4)
func New(dbPath string, reg *registry.Service, runner BacktestRunner, workers int) (*Service, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("optimizer: open db: %w", err)
	}
	s := &Service{
		db:       db,
		reg:      reg,
		runner:   runner,
		promoter: NewPromoter(reg),
		workers:  workers,
	}
	if s.workers <= 0 {
		s.workers = 4
	}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) migrate() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS opt_jobs (
		job_id     TEXT PRIMARY KEY,
		strategy_id TEXT NOT NULL,
		strategy_version TEXT NOT NULL,
		status     TEXT NOT NULL DEFAULT 'pending',
		created_at INTEGER NOT NULL,
		completed_at INTEGER,
		payload    TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_opt_status ON opt_jobs(status);
	`)
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// Public API
// ─────────────────────────────────────────────────────────────────────────────

// Submit creates and persists a new optimization job.
func (s *Service) Submit(
	strategyID, strategyVersion, createdBy string,
	trainFrom, trainTo, valFrom, valTo time.Time,
	thresholds PromotionThresholds,
	maxCandidates int,
) (*OptimizationJob, error) {
	// Verify strategy exists
	parent, err := s.reg.GetVersion(strategyID, strategyVersion)
	if err != nil {
		return nil, fmt.Errorf("optimizer: parent strategy: %w", err)
	}
	_ = parent

	job := &OptimizationJob{
		JobID:           uuid.NewString(),
		StrategyID:      strategyID,
		StrategyVersion: strategyVersion,
		CreatedAt:       time.Now(),
		CreatedBy:       createdBy,
		Thresholds:      thresholds,
		TrainFrom:       trainFrom,
		TrainTo:         trainTo,
		ValFrom:         valFrom,
		ValTo:           valTo,
		MaxCandidates:   maxCandidates,
		Status:          "pending",
	}
	if maxCandidates <= 0 {
		job.MaxCandidates = 20
	}

	if err := s.persistJob(job); err != nil {
		return nil, err
	}
	return job, nil
}

// Run executes the job synchronously.  It is meant to be called from a
// background goroutine or worker loop.
func (s *Service) Run(ctx context.Context, jobID string) error {
	job, err := s.loadJob(jobID)
	if err != nil {
		return err
	}
	if job.Status != "pending" {
		return fmt.Errorf("optimizer: job %s status is %s, want pending", jobID, job.Status)
	}

	job.Status = "running"
	s.persistJob(job)

	parent, err := s.reg.GetVersion(job.StrategyID, job.StrategyVersion)
	if err != nil {
		return s.failJob(job, fmt.Errorf("load parent strategy: %w", err))
	}

	// ── 1. Generate candidates ────────────────────────────────────────────────
	candidates := GenerateCandidates(parent, job.MaxCandidates)
	log.Printf("optimizer: job %s generated %d candidates", jobID, len(candidates))

	// ── 2. Evaluate candidates in parallel ────────────────────────────────────
	candidates = s.evaluateParallel(ctx, job, candidates)

	// ── 3. Sort by score descending ───────────────────────────────────────────
	sort.Slice(candidates, func(i, j int) bool {
		si, sj := -1e9, -1e9
		if candidates[i].EvalResult != nil {
			si = candidates[i].EvalResult.Score
		}
		if candidates[j].EvalResult != nil {
			sj = candidates[j].EvalResult.Score
		}
		return si > sj
	})

	// ── 4. Promote passing candidates ─────────────────────────────────────────
	promoted, err := s.promoter.Promote(parent, candidates, job.CreatedBy)
	if err != nil {
		log.Printf("optimizer: job %s promote error: %v", jobID, err)
	}
	job.PromotedCount = len(promoted)

	// ── 5. Persist final state ────────────────────────────────────────────────
	job.Candidates = candidates
	job.Status = "done"
	now := time.Now()
	job.CompletedAt = &now
	s.persistJob(job)

	log.Printf("optimizer: job %s done. evaluated=%d promoted=%d", jobID, len(candidates), len(promoted))
	return nil
}

// GetJob returns a job by ID.
func (s *Service) GetJob(jobID string) (*OptimizationJob, error) {
	return s.loadJob(jobID)
}

// ListJobs returns all jobs for a strategy.
func (s *Service) ListJobs(strategyID string) ([]*OptimizationJob, error) {
	rows, err := s.db.Query(
		`SELECT payload FROM opt_jobs WHERE strategy_id=? ORDER BY created_at DESC`, strategyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OptimizationJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Close releases resources.
func (s *Service) Close() error { return s.db.Close() }

// ─────────────────────────────────────────────────────────────────────────────
// Internal
// ─────────────────────────────────────────────────────────────────────────────

func (s *Service) evaluateParallel(ctx context.Context, job *OptimizationJob, candidates []Candidate) []Candidate {
	type result struct {
		idx int
		c   Candidate
	}

	sem := make(chan struct{}, s.workers)
	results := make(chan result, len(candidates))
	var wg sync.WaitGroup

	for i, c := range candidates {
		wg.Add(1)
		go func(idx int, cand Candidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			evalResult, err := EvaluateCandidate(
				ctx, &cand, s.runner,
				job.TrainFrom, job.TrainTo,
				job.ValFrom, job.ValTo,
				job.Thresholds,
			)
			if err != nil {
				log.Printf("optimizer: evaluate candidate %s: %v", cand.CandidateID, err)
			} else {
				cand.EvalResult = evalResult
			}
			results <- result{idx: idx, c: cand}
		}(i, c)
	}

	wg.Wait()
	close(results)

	out := make([]Candidate, len(candidates))
	for r := range results {
		out[r.idx] = r.c
	}
	return out
}

func (s *Service) persistJob(job *OptimizationJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO opt_jobs(job_id,strategy_id,strategy_version,status,created_at,completed_at,payload)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(job_id) DO UPDATE SET status=excluded.status, completed_at=excluded.completed_at, payload=excluded.payload
	`,
		job.JobID, job.StrategyID, job.StrategyVersion, job.Status,
		job.CreatedAt.UnixMilli(),
		nullableTime(job.CompletedAt),
		string(payload),
	)
	return err
}

func (s *Service) loadJob(jobID string) (*OptimizationJob, error) {
	row := s.db.QueryRow(`SELECT payload FROM opt_jobs WHERE job_id=?`, jobID)
	return scanJob(row)
}

func (s *Service) failJob(job *OptimizationJob, err error) error {
	job.Status = "failed"
	s.persistJob(job)
	return err
}

type rowScannerJ interface {
	Scan(dest ...any) error
}

func scanJob(row rowScannerJ) (*OptimizationJob, error) {
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("optimizer: job not found")
		}
		return nil, err
	}
	var j OptimizationJob
	return &j, json.Unmarshal([]byte(payload), &j)
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
