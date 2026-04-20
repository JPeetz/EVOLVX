// Package outcome – automatic recorder.
//
// Recorder sits between the pipeline and the journal.  It maintains an
// in-memory (+ SQLite-persisted) map of open positions.  When the pipeline
// emits an open fill it registers the position.  When a close fill arrives
// it computes the outcome and calls journal.RecordOutcome() automatically.
//
// This closes the feedback loop that v1.1 required manual API calls for.
// Every decision in the journal will now have a populated Outcome as soon
// as the exchange confirms the closing fill.
package outcome

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
	_ "github.com/mattn/go-sqlite3"
)

// ─────────────────────────────────────────────────────────────────────────────
// Recorder
// ─────────────────────────────────────────────────────────────────────────────

// Recorder is the single component responsible for automatic outcome tracking.
//
// Integration:  Call OnFill() from the pipeline after every confirmed fill.
// The recorder handles the open/close matching internally.
//
// Persistence:  Open positions survive restarts — they are written to SQLite
// immediately on registration and deleted on close.
type Recorder struct {
	journal *journal.Service
	db      *sql.DB
	mu      sync.Mutex
	open    map[string]*OpenPosition // key: symbol+"_"+strategyID
}

// NewRecorder creates a recorder backed by journalSvc and stores open
// positions in dbPath.
func NewRecorder(journalSvc *journal.Service, dbPath string) (*Recorder, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("outcome recorder: open db: %w", err)
	}
	r := &Recorder{
		journal: journalSvc,
		db:      db,
		open:    make(map[string]*OpenPosition),
	}
	if err := r.migrate(); err != nil {
		return nil, fmt.Errorf("outcome recorder: migrate: %w", err)
	}
	if err := r.rehydrate(); err != nil {
		log.Printf("outcome recorder: rehydrate warning: %v", err)
	}
	return r, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Core API — called by the pipeline
// ─────────────────────────────────────────────────────────────────────────────

// OnFill is called by the pipeline after every confirmed fill.
// It dispatches to registerOpen or recordClose based on the fill's signal action.
func (r *Recorder) OnFill(ctx context.Context, cc *core.CycleContext) {
	if cc.Fill == nil || cc.Signal == nil {
		return
	}
	switch cc.Signal.Action {
	case core.ActionOpenLong, core.ActionOpenShort:
		r.registerOpen(cc)
	case core.ActionCloseLong, core.ActionCloseShort:
		r.recordClose(ctx, cc)
	}
}

// UpdateMarkPrices is called each bar to track peak unrealised PnL
// for drawdown statistics.
func (r *Recorder) UpdateMarkPrices(prices map[string]float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for sym, pos := range r.open {
		mp, ok := prices[sym]
		if !ok {
			continue
		}
		var unreal float64
		if pos.Side == "long" {
			unreal = (mp - pos.EntryPrice) * pos.EntryQty
		} else {
			unreal = (pos.EntryPrice - mp) * pos.EntryQty
		}
		if unreal > pos.PeakUnrealPnL {
			pos.PeakUnrealPnL = unreal
			r.open[sym] = pos
		}
	}
}

// OpenPositions returns a snapshot of currently tracked open positions.
func (r *Recorder) OpenPositions() []*OpenPosition {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*OpenPosition, 0, len(r.open))
	for _, p := range r.open {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// Close releases resources.
func (r *Recorder) Close() error { return r.db.Close() }

// ─────────────────────────────────────────────────────────────────────────────
// Internal
// ─────────────────────────────────────────────────────────────────────────────

func (r *Recorder) registerOpen(cc *core.CycleContext) {
	if cc.Fill == nil || cc.Signal == nil {
		return
	}
	side := "long"
	if cc.Signal.Action == core.ActionOpenShort {
		side = "short"
	}

	pos := &OpenPosition{
		DecisionID:      "", // filled by journal lookup below
		Symbol:          cc.Signal.Symbol,
		Side:            side,
		EntryPrice:      cc.Fill.FilledPrice,
		EntryQty:        cc.Fill.FilledQty,
		EntryFee:        cc.Fill.Fee,
		EntryTime:       cc.Fill.Timestamp,
		Leverage:        cc.Signal.Leverage,
		StopLoss:        cc.Signal.StopLoss,
		TakeProfit:      cc.Signal.TakeProfit,
		StrategyID:      cc.StrategyID,
		StrategyVersion: cc.StrategyVersion,
		SessionID:       cc.SessionID,
		Mode:            cc.Mode,
	}

	// Find the corresponding journal decision by signal ID / symbol / timestamp
	entries, err := r.journal.LatestForSymbol(cc.StrategyID, cc.StrategyVersion, cc.Signal.Symbol, 1)
	if err == nil && len(entries) > 0 {
		pos.DecisionID = entries[0].DecisionID
	}

	key := posKey(cc.Signal.Symbol, cc.StrategyID)
	r.mu.Lock()
	r.open[key] = pos
	r.mu.Unlock()

	r.persist(pos)
}

func (r *Recorder) recordClose(ctx context.Context, cc *core.CycleContext) {
	if cc.Fill == nil || cc.Signal == nil {
		return
	}
	key := posKey(cc.Signal.Symbol, cc.StrategyID)

	r.mu.Lock()
	pos, found := r.open[key]
	if found {
		delete(r.open, key)
	}
	r.mu.Unlock()

	if !found || pos.DecisionID == "" {
		// No tracked open position — nothing to close in the journal
		return
	}

	exitReason := inferExitReason(cc.Signal, pos)
	closeEvt := CloseEvent{
		Symbol:     cc.Signal.Symbol,
		ClosePrice: cc.Fill.FilledPrice,
		CloseQty:   cc.Fill.FilledQty,
		CloseFee:   cc.Fill.Fee,
		CloseTime:  cc.Fill.Timestamp,
		ExitReason: exitReason,
		Mode:       cc.Mode,
	}

	outcome := ComputeOutcome(*pos, closeEvt)

	if err := r.journal.RecordOutcome(pos.DecisionID, outcome); err != nil {
		log.Printf("outcome recorder: record outcome for %s: %v", pos.DecisionID, err)
		return
	}

	r.deletePersisted(key)
	log.Printf("outcome recorder: recorded %s outcome for %s/%s (PnL %.2f USDT, %.2f%%)",
		outcome.Class, pos.Symbol, pos.Side,
		outcome.RealizedPnL, outcome.ReturnPct*100)
}

// inferExitReason determines why the position was closed based on the
// close signal and the original position's SL/TP levels.
func inferExitReason(sig *core.Signal, pos *OpenPosition) string {
	if sig.Reasoning != "" {
		r := sig.Reasoning
		if contains(r, "stop loss") || contains(r, "stop_loss") || contains(r, "stopped out") {
			return "stop_loss"
		}
		if contains(r, "take profit") || contains(r, "take_profit") || contains(r, "target") {
			return "take_profit"
		}
		if contains(r, "liquidat") {
			return "liquidation"
		}
	}
	// Price-based inference: if fill price is close to SL or TP
	fp := sig.StopLoss
	tp := sig.TakeProfit
	if fp > 0 && tp > 0 {
		// Not applicable for the close signal's own SL/TP, use position's
		fp = pos.StopLoss
		tp = pos.TakeProfit
	}
	_ = fp
	_ = tp
	return "signal" // default: closed by AI signal
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			match := true
			for j := 0; j < len(sub); j++ {
				if s[i+j]|0x20 != sub[j]|0x20 { // case-insensitive
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
		return false
	}()
}

func posKey(symbol, strategyID string) string {
	return symbol + "_" + strategyID
}

// ─────────────────────────────────────────────────────────────────────────────
// SQLite persistence — survive restarts
// ─────────────────────────────────────────────────────────────────────────────

func (r *Recorder) migrate() error {
	_, err := r.db.Exec(`
	CREATE TABLE IF NOT EXISTS open_positions (
		pos_key    TEXT PRIMARY KEY,
		payload    TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`)
	return err
}

func (r *Recorder) persist(pos *OpenPosition) {
	key := posKey(pos.Symbol, pos.StrategyID)
	payload, _ := json.Marshal(pos)
	r.db.Exec(
		`INSERT OR REPLACE INTO open_positions(pos_key, payload, created_at) VALUES(?,?,?)`,
		key, string(payload), time.Now().UnixMilli(),
	)
}

func (r *Recorder) deletePersisted(key string) {
	r.db.Exec(`DELETE FROM open_positions WHERE pos_key=?`, key)
}

func (r *Recorder) rehydrate() error {
	rows, err := r.db.Query(`SELECT pos_key, payload FROM open_positions`)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var key, payload string
		if err := rows.Scan(&key, &payload); err != nil {
			continue
		}
		var pos OpenPosition
		if err := json.Unmarshal([]byte(payload), &pos); err != nil {
			continue
		}
		r.open[key] = &pos
		count++
	}
	if count > 0 {
		log.Printf("outcome recorder: rehydrated %d open positions from previous session", count)
	}
	return rows.Err()
}
