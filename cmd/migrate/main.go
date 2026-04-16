// cmd/migrate/main.go
//
// One-time migration tool.
//
// Usage:
//   go run ./cmd/migrate/main.go \
//     --legacy-db    ./nofx.db          \
//     --registry-db  ./registry.db      \
//     --journal-db   ./journal.db       \
//     [--import-decisions]
//
// It is safe to run multiple times (idempotent).
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	legacyDB := flag.String("legacy-db", "./nofx.db", "path to existing nofx.db")
	registryDB := flag.String("registry-db", "./registry.db", "path to new registry.db")
	journalDB := flag.String("journal-db", "./journal.db", "path to new journal.db")
	importDecisions := flag.Bool("import-decisions", false, "also import legacy decision_records")
	flag.Parse()

	// ── 1. Open registry ──────────────────────────────────────────────────────
	reg, err := registry.New(*registryDB)
	if err != nil {
		log.Fatalf("open registry: %v", err)
	}
	defer reg.Close()

	// ── 2. Import strategies ──────────────────────────────────────────────────
	log.Printf("importing strategies from %s ...", *legacyDB)
	if err := registry.MigrateFromLegacy(*legacyDB, reg); err != nil {
		log.Fatalf("migrate strategies: %v", err)
	}

	// ── 3. Optionally import legacy decision records ──────────────────────────
	if *importDecisions {
		log.Printf("importing decision records from %s ...", *legacyDB)
		if err := importLegacyDecisions(*legacyDB, *journalDB, reg); err != nil {
			log.Printf("WARNING: decision import error: %v", err)
		}
	}

	log.Println("migration complete.")
}

// importLegacyDecisions reads the existing decision_records table and converts
// each row into a journal.DecisionEntry.
func importLegacyDecisions(legacyPath, journalPath string, reg *registry.Service) error {
	legacy, err := sql.Open("sqlite3", legacyPath)
	if err != nil {
		return fmt.Errorf("open legacy: %w", err)
	}
	defer legacy.Close()

	j, err := journal.New(journalPath)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer j.Close()

	// The existing table name is "decision_logs" (or "decision_records" in some versions).
	// We try both.
	rows, err := legacy.Query(`
		SELECT id, trader_id, timestamp, system_prompt, input_prompt,
		       cot_trace, decision_json, raw_response, execution_log, success
		FROM   decision_logs
		ORDER  BY id ASC
	`)
	if err != nil {
		// Try alternate name
		rows, err = legacy.Query(`
			SELECT id, trader_id, timestamp, system_prompt, input_prompt,
			       cot_trace, decision_json, raw_response, execution_log, success
			FROM   decision_records
			ORDER  BY id ASC
		`)
		if err != nil {
			return fmt.Errorf("query legacy decisions: %w", err)
		}
	}
	defer rows.Close()

	imported := 0
	for rows.Next() {
		var (
			id, traderID, tsStr     string
			systemPrompt, userPrompt string
			cotTrace, decisionJSON  string
			rawResponse, execLog    string
			success                 bool
		)
		if err := rows.Scan(&id, &traderID, &tsStr, &systemPrompt, &userPrompt,
			&cotTrace, &decisionJSON, &rawResponse, &execLog, &success); err != nil {
			log.Printf("scan row %s: %v", id, err)
			continue
		}

		ts, _ := time.Parse(time.RFC3339, tsStr)
		if ts.IsZero() {
			ts = time.Now()
		}

		// Try to find the strategy in the registry by trader ID
		strategyID, strategyVersion := resolveStrategy(reg, traderID)

		// Parse the decision JSON to extract symbol and action
		symbol, action, confidence := parseDecisionJSON(decisionJSON)

		entry := &journal.DecisionEntry{
			DecisionID:      uuid.NewString(),
			StrategyID:      strategyID,
			StrategyVersion: strategyVersion,
			SessionID:       traderID,
			CycleNumber:     0,
			Symbol:          symbol,
			Timestamp:       ts,
			Mode:            core.ModeLive,
			Action:          core.SignalAction(action),
			Confidence:      confidence,
			Reasoning:       cotTrace,
			RawAIResponse:   rawResponse,
			CoTTrace:        cotTrace,
			MarketSnapshot:  journal.MarketSnapshot{},
			RiskState:       journal.RiskSnapshot{Approved: success},
		}

		if err := j.Record(entry); err != nil {
			log.Printf("record decision %s: %v", id, err)
			continue
		}
		imported++
	}

	log.Printf("imported %d legacy decision records", imported)
	return rows.Err()
}

func resolveStrategy(reg *registry.Service, traderID string) (string, string) {
	strategies, err := reg.ListByStatus(registry.StatusDraft)
	if err != nil || len(strategies) == 0 {
		return traderID, "1.0.0"
	}
	// Best-effort: return the first strategy (migration is approximate)
	return strategies[0].ID, strategies[0].Version
}

func parseDecisionJSON(raw string) (symbol, action string, confidence int) {
	var decisions []struct {
		Symbol     string `json:"symbol"`
		Action     string `json:"action"`
		Confidence int    `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw), &decisions); err != nil || len(decisions) == 0 {
		return "UNKNOWN", "hold", 0
	}
	return decisions[0].Symbol, decisions[0].Action, decisions[0].Confidence
}

var _ = os.Stderr
