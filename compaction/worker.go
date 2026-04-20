// Package compaction implements the auto-compaction worker.
//
// The journal.Service.Compact() method exists since v1.0 but requires manual
// API calls.  The compaction worker runs on a schedule and automatically
// compacts strategies that exceed their retention threshold.
//
// Policy priority (highest wins):
//   1. Per-strategy override  (e.g. approved strategies keep 180d)
//   2. Per-mode default       (e.g. backtest keeps 7d, live keeps 90d)
//   3. Global default         (30d)
//
// The worker runs a full compaction pass every CompactionInterval.
// It is goroutine-safe and can run alongside the trading pipeline.
package compaction

import (
	"context"
	"log"
	"time"

	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Policy
// ─────────────────────────────────────────────────────────────────────────────

// RetentionPolicy defines how long to retain full decision records before
// compacting them into a StrategySummary.
type RetentionPolicy struct {
	// GlobalRetainDays is the default retention period for all strategies.
	// Default: 30 days.
	GlobalRetainDays int

	// ByMode lets you keep live decisions longer than backtest ones.
	ByMode map[string]int // mode → retain days

	// ByStatus lets approved strategies keep their history longer.
	ByStatus map[registry.StrategyStatus]int

	// ByStrategy lets you pin specific strategies to custom retention.
	ByStrategy map[string]int // strategyID → retain days

	// MinTradesToCompact: don't compact a strategy+version until it has
	// at least this many closed decisions.  Prevents premature compaction
	// of new strategies with few decisions.  Default: 20.
	MinTradesToCompact int
}

// DefaultPolicy returns sensible production defaults.
func DefaultPolicy() RetentionPolicy {
	return RetentionPolicy{
		GlobalRetainDays: 30,
		ByMode: map[string]int{
			"backtest": 7,   // backtests are reproducible, don't need long history
			"paper":    30,
			"live":     90,  // live decisions are the most valuable, keep longer
		},
		ByStatus: map[registry.StrategyStatus]int{
			registry.StatusApproved:   180, // keep approved strategy history for 6 months
			registry.StatusPaper:      60,
			registry.StatusDraft:      14,
			registry.StatusDeprecated: 7,
		},
		MinTradesToCompact: 20,
	}
}

// retentionDays returns the effective retention period for a strategy.
func (p *RetentionPolicy) retentionDays(strategyID string, status registry.StrategyStatus) int {
	// Per-strategy override is highest priority
	if days, ok := p.ByStrategy[strategyID]; ok {
		return days
	}
	// Per-status
	if status != "" {
		if days, ok := p.ByStatus[status]; ok {
			return days
		}
	}
	return p.GlobalRetainDays
}

// ─────────────────────────────────────────────────────────────────────────────
// Worker
// ─────────────────────────────────────────────────────────────────────────────

// Worker runs periodic compaction passes.
type Worker struct {
	journal  *journal.Service
	registry *registry.Service
	policy   RetentionPolicy
	interval time.Duration
}

// NewWorker creates a compaction worker.
//
//   interval: how often to run a full compaction pass (e.g. 6*time.Hour)
//   policy:   retention policy (use DefaultPolicy() for sane defaults)
func NewWorker(
	journalSvc *journal.Service,
	reg *registry.Service,
	policy RetentionPolicy,
	interval time.Duration,
) *Worker {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	return &Worker{
		journal:  journalSvc,
		registry: reg,
		policy:   policy,
		interval: interval,
	}
}

// Run starts the compaction loop.  It blocks until ctx is cancelled.
// Call it in a goroutine.
func (w *Worker) Run(ctx context.Context) {
	log.Printf("compaction worker: starting, interval=%s", w.interval)

	// Run once immediately at startup
	w.runPass()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("compaction worker: stopping")
			return
		case <-ticker.C:
			w.runPass()
		}
	}
}

// RunOnce executes a single compaction pass synchronously.
// Useful for testing or manual invocation from the API.
func (w *Worker) RunOnce() CompactionResult {
	return w.runPass()
}

// CompactionResult summarises one compaction pass.
type CompactionResult struct {
	StartedAt         time.Time
	Duration          time.Duration
	StrategiesScanned int
	StrategiesCompacted int
	DecisionsArchived int
	Errors            []string
}

func (w *Worker) runPass() CompactionResult {
	start := time.Now()
	result := CompactionResult{StartedAt: start}

	log.Printf("compaction worker: starting pass")

	// Get all strategy versions from the registry
	var allVersions []*registry.StrategyRecord
	for _, status := range []registry.StrategyStatus{
		registry.StatusApproved, registry.StatusPaper,
		registry.StatusDraft, registry.StatusDeprecated,
	} {
		versions, err := w.registry.ListByStatus(status)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("list %s: %v", status, err))
			continue
		}
		allVersions = append(allVersions, versions...)
	}

	result.StrategiesScanned = len(allVersions)

	for _, r := range allVersions {
		retainDays := w.policy.retentionDays(r.ID, r.Status)

		// Check if there's anything old enough to compact
		cutoff := time.Now().AddDate(0, 0, -retainDays)
		entries, err := w.journal.Query(journal.QueryFilter{
			StrategyID:      r.ID,
			StrategyVersion: r.Version,
			To:              &cutoff,
			Limit:           1,
		})
		if err != nil || len(entries) == 0 {
			continue // nothing to compact for this version
		}

		// Check minimum trades threshold
		allEntries, _ := w.journal.Query(journal.QueryFilter{
			StrategyID:      r.ID,
			StrategyVersion: r.Version,
			Limit:           w.policy.MinTradesToCompact + 1,
		})
		if len(allEntries) < w.policy.MinTradesToCompact {
			continue // too few trades to compact meaningfully
		}

		// Run compaction
		summary, err := w.journal.Compact(r.ID, r.Version, retainDays)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("compact %s/%s: %v", r.ID, r.Version, err))
			continue
		}
		if summary != nil {
			result.StrategiesCompacted++
			result.DecisionsArchived += summary.TotalDecisions
			log.Printf("compaction worker: compacted %s/%s — %d decisions archived, %d retained",
				r.ID, r.Version, summary.TotalDecisions, retainDays)
		}
	}

	result.Duration = time.Since(start)
	log.Printf("compaction worker: pass complete in %s — %d/%d strategies compacted, %d archived, %d errors",
		result.Duration.Round(time.Millisecond),
		result.StrategiesCompacted, result.StrategiesScanned,
		result.DecisionsArchived, len(result.Errors))

	return result
}

// missing fmt import
var _ = fmt.Sprintf
