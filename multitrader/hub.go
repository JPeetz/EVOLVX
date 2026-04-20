// Package multitrader implements the SharedJournalHub.
//
// NOFX runs multiple auto-traders simultaneously (different strategies, same
// process).  In v1.x each trader writes to its own journal instance.  In v2.0
// all traders write through a single hub so decisions can be correlated across
// strategies — which is the prerequisite for cross-strategy attribution and
// symbol-level memory consolidation.
//
// The hub is a thin write-multiplexer + event broadcaster:
//
//   trader A  ──┐
//   trader B  ──┤──► SharedJournalHub ──► journal.Service (one DB)
//   trader C  ──┘                    └──► Subscribers (attribution, symbol memory)
//
// All hub operations are goroutine-safe.  Write latency is < 1ms because
// the hub dispatches subscriber notifications asynchronously.
package multitrader

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/NoFxAiOS/nofx/journal"
)

// ─────────────────────────────────────────────────────────────────────────────
// Subscriber — anything that wants to react to journal events
// ─────────────────────────────────────────────────────────────────────────────

// DecisionSubscriber is notified when a new decision is recorded.
type DecisionSubscriber interface {
	OnDecision(entry *journal.DecisionEntry)
}

// OutcomeSubscriber is notified when an outcome is recorded.
type OutcomeSubscriber interface {
	OnOutcome(decisionID string, outcome journal.Outcome)
}

// ─────────────────────────────────────────────────────────────────────────────
// TraderRegistration
// ─────────────────────────────────────────────────────────────────────────────

// TraderRegistration holds metadata about one registered trader.
type TraderRegistration struct {
	TraderID        string
	StrategyID      string
	StrategyVersion string
	Name            string
}

// ─────────────────────────────────────────────────────────────────────────────
// SharedJournalHub
// ─────────────────────────────────────────────────────────────────────────────

// SharedJournalHub is the single point through which all traders write
// to the journal and through which all consumers receive events.
type SharedJournalHub struct {
	journal *journal.Service

	mu      sync.RWMutex
	traders map[string]*TraderRegistration // traderID → registration

	decisionSubs []DecisionSubscriber
	outcomeSubs  []OutcomeSubscriber
}

// NewSharedJournalHub creates a hub backed by journalSvc.
func NewSharedJournalHub(journalSvc *journal.Service) *SharedJournalHub {
	return &SharedJournalHub{
		journal: journalSvc,
		traders: make(map[string]*TraderRegistration),
	}
}

// ─── Trader lifecycle ─────────────────────────────────────────────────────────

// Register declares a trader as active.  Must be called before the trader
// starts writing decisions.
func (h *SharedJournalHub) Register(reg TraderRegistration) error {
	if reg.TraderID == "" {
		return fmt.Errorf("hub: trader ID cannot be empty")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.traders[reg.TraderID] = &reg
	log.Printf("hub: registered trader %s (%s v%s)", reg.TraderID, reg.StrategyID, reg.StrategyVersion)
	return nil
}

// Deregister removes a trader from the active set.
func (h *SharedJournalHub) Deregister(traderID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.traders, traderID)
	log.Printf("hub: deregistered trader %s", traderID)
}

// ActiveTraders returns a snapshot of all currently registered traders.
func (h *SharedJournalHub) ActiveTraders() []TraderRegistration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]TraderRegistration, 0, len(h.traders))
	for _, t := range h.traders {
		out = append(out, *t)
	}
	return out
}

// ─── Write API (called by each trader instead of journal.Record directly) ────

// Record writes a decision to the shared journal and notifies subscribers.
func (h *SharedJournalHub) Record(entry *journal.DecisionEntry) error {
	if err := h.journal.Record(entry); err != nil {
		return fmt.Errorf("hub: record decision: %w", err)
	}
	h.notifyDecision(entry)
	return nil
}

// RecordOutcome writes an outcome and notifies subscribers.
func (h *SharedJournalHub) RecordOutcome(decisionID string, outcome journal.Outcome) error {
	if err := h.journal.RecordOutcome(decisionID, outcome); err != nil {
		return fmt.Errorf("hub: record outcome: %w", err)
	}
	h.notifyOutcome(decisionID, outcome)
	return nil
}

// ─── Read API (pass-through to journal) ──────────────────────────────────────

// Query forwards to journal.Query.
func (h *SharedJournalHub) Query(f journal.QueryFilter) ([]*journal.DecisionEntry, error) {
	return h.journal.Query(f)
}

// LatestForSymbol returns the most recent decisions for symbol across ALL
// strategies, not just one.  This is the key cross-strategy read that the
// SymbolMemoryStore and attribution engine rely on.
func (h *SharedJournalHub) LatestForSymbolAcrossStrategies(symbol string, n int) ([]*journal.DecisionEntry, error) {
	if n <= 0 {
		n = 20
	}
	return h.journal.Query(journal.QueryFilter{
		Symbol: symbol,
		Limit:  n,
	})
}

// ─── Subscription ─────────────────────────────────────────────────────────────

// SubscribeDecisions adds a subscriber that is called for every new decision.
// Callbacks run in a separate goroutine to avoid blocking writers.
func (h *SharedJournalHub) SubscribeDecisions(s DecisionSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.decisionSubs = append(h.decisionSubs, s)
}

// SubscribeOutcomes adds a subscriber for outcome recordings.
func (h *SharedJournalHub) SubscribeOutcomes(s OutcomeSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.outcomeSubs = append(h.outcomeSubs, s)
}

func (h *SharedJournalHub) notifyDecision(entry *journal.DecisionEntry) {
	h.mu.RLock()
	subs := make([]DecisionSubscriber, len(h.decisionSubs))
	copy(subs, h.decisionSubs)
	h.mu.RUnlock()

	for _, s := range subs {
		s := s
		e := *entry // copy for goroutine safety
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("hub: decision subscriber panic: %v", r)
				}
			}()
			s.OnDecision(&e)
		}()
	}
}

func (h *SharedJournalHub) notifyOutcome(decisionID string, outcome journal.Outcome) {
	h.mu.RLock()
	subs := make([]OutcomeSubscriber, len(h.outcomeSubs))
	copy(subs, h.outcomeSubs)
	h.mu.RUnlock()

	for _, s := range subs {
		s := s
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("hub: outcome subscriber panic: %v", r)
				}
			}()
			s.OnOutcome(decisionID, outcome)
		}()
	}
}

// ─── Stats ────────────────────────────────────────────────────────────────────

// HubStats returns a snapshot of hub activity.
type HubStats struct {
	ActiveTraderCount    int
	DecisionSubscribers  int
	OutcomeSubscribers   int
}

// Stats returns current hub statistics.
func (h *SharedJournalHub) Stats() HubStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return HubStats{
		ActiveTraderCount:   len(h.traders),
		DecisionSubscribers: len(h.decisionSubs),
		OutcomeSubscribers:  len(h.outcomeSubs),
	}
}

// keep unused context import out
var _ = context.Background
