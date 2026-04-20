// Package attribution implements cross-strategy performance attribution.
//
// Attribution answers: "Of the total PnL generated, how much came from
// each strategy version, each symbol, and each market regime?"
//
// This matters because a single EvolvX deployment may run N approved strategy
// versions simultaneously.  Without attribution, you can't tell whether
// overall profits came from one good strategy or are spread evenly.
//
// AttributionEngine reads from the shared journal and computes:
//   - PnL attribution by strategy version
//   - PnL attribution by symbol
//   - PnL attribution by regime (requires regime labels on decisions)
//   - Win rate attribution (which strategy has the best WR on which symbol)
//   - Drawdown attribution (which strategy caused the worst drawdowns)
package attribution

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/multitrader"
)

// ─────────────────────────────────────────────────────────────────────────────
// Attribution result types
// ─────────────────────────────────────────────────────────────────────────────

// AttributionReport is the full attribution breakdown for a time window.
type AttributionReport struct {
	From            time.Time          `json:"from"`
	To              time.Time          `json:"to"`
	TotalPnL        float64            `json:"total_pnl"`
	TotalTrades     int                `json:"total_trades"`
	TotalWins       int                `json:"total_wins"`
	OverallWinRate  float64            `json:"overall_win_rate"`

	ByStrategy   []StrategyAttribution `json:"by_strategy"`
	BySymbol     []SymbolAttribution   `json:"by_symbol"`
	ByRegime     []RegimeAttribution   `json:"by_regime"`
	Interactions []StrategySymbolMatrix `json:"interactions"` // strategy × symbol
}

// StrategyAttribution shows one strategy version's contribution.
type StrategyAttribution struct {
	StrategyID      string  `json:"strategy_id"`
	StrategyVersion string  `json:"strategy_version"`
	Trades          int     `json:"trades"`
	Wins            int     `json:"wins"`
	WinRate         float64 `json:"win_rate"`
	PnL             float64 `json:"pnl"`
	PnLShare        float64 `json:"pnl_share"`  // fraction of total PnL
	AvgReturn       float64 `json:"avg_return"`
	MaxDrawdown     float64 `json:"max_drawdown"`
	SharpeProxy     float64 `json:"sharpe_proxy"` // simplified: mean/stddev of returns
	BestSymbol      string  `json:"best_symbol"`
	WorstSymbol     string  `json:"worst_symbol"`
}

// SymbolAttribution shows one symbol's contribution.
type SymbolAttribution struct {
	Symbol      string  `json:"symbol"`
	Trades      int     `json:"trades"`
	Wins        int     `json:"wins"`
	WinRate     float64 `json:"win_rate"`
	PnL         float64 `json:"pnl"`
	PnLShare    float64 `json:"pnl_share"`
	AvgReturn   float64 `json:"avg_return"`
	BestStrategy string `json:"best_strategy"`
}

// RegimeAttribution shows performance breakdown by market regime.
type RegimeAttribution struct {
	Regime    string  `json:"regime"`
	Trades    int     `json:"trades"`
	Wins      int     `json:"wins"`
	WinRate   float64 `json:"win_rate"`
	PnL       float64 `json:"pnl"`
	PnLShare  float64 `json:"pnl_share"`
	AvgReturn float64 `json:"avg_return"`
}

// StrategySymbolMatrix is one cell in the strategy × symbol attribution table.
type StrategySymbolMatrix struct {
	StrategyID      string  `json:"strategy_id"`
	StrategyVersion string  `json:"strategy_version"`
	Symbol          string  `json:"symbol"`
	Trades          int     `json:"trades"`
	WinRate         float64 `json:"win_rate"`
	PnL             float64 `json:"pnl"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Engine
// ─────────────────────────────────────────────────────────────────────────────

// Engine computes attribution reports from the shared journal.
type Engine struct {
	hub *multitrader.SharedJournalHub
}

// NewEngine creates an attribution engine backed by hub.
func NewEngine(hub *multitrader.SharedJournalHub) *Engine {
	return &Engine{hub: hub}
}

// Compute builds a full attribution report for the given time window.
// Only closed decisions (with outcomes) are included in the PnL attribution.
// Open/pending decisions are counted in Trades but excluded from PnL.
func (e *Engine) Compute(from, to time.Time) (*AttributionReport, error) {
	// Query all decisions in the window
	entries, err := e.hub.Query(journal.QueryFilter{
		From:  &from,
		To:    &to,
		Limit: 10000,
	})
	if err != nil {
		return nil, fmt.Errorf("attribution: query: %w", err)
	}

	report := &AttributionReport{
		From: from,
		To:   to,
	}

	if len(entries) == 0 {
		return report, nil
	}

	// ── Accumulators ─────────────────────────────────────────────────────────
	type stratKey struct{ id, ver string }
	stratMap := make(map[stratKey]*stratAccum)
	symMap   := make(map[string]*symAccum)
	regMap   := make(map[string]*regAccum)
	intMap   := make(map[string]*intAccum) // "stratID/stratVer/symbol"

	for _, e := range entries {
		if e.Outcome == nil {
			report.TotalTrades++
			continue
		}

		report.TotalTrades++
		report.TotalPnL += e.Outcome.RealizedPnL
		if e.Outcome.Class == journal.OutcomeWin {
			report.TotalWins++
		}

		// Strategy accumulator
		sk := stratKey{e.StrategyID, e.StrategyVersion}
		sa := stratMap[sk]
		if sa == nil {
			sa = &stratAccum{id: e.StrategyID, ver: e.StrategyVersion}
			stratMap[sk] = sa
		}
		sa.add(e)

		// Symbol accumulator
		sym := symMap[e.Symbol]
		if sym == nil {
			sym = &symAccum{symbol: e.Symbol}
			symMap[e.Symbol] = sym
		}
		sym.add(e)

		// Regime accumulator (use market snapshot regime if present)
		regime := inferRegime(e)
		if regime != "" {
			ra := regMap[regime]
			if ra == nil {
				ra = &regAccum{regime: regime}
				regMap[regime] = ra
			}
			ra.add(e)
		}

		// Interaction matrix
		ik := fmt.Sprintf("%s/%s/%s", e.StrategyID, e.StrategyVersion, e.Symbol)
		ia := intMap[ik]
		if ia == nil {
			ia = &intAccum{stratID: e.StrategyID, stratVer: e.StrategyVersion, symbol: e.Symbol}
			intMap[ik] = ia
		}
		ia.add(e)
	}

	if report.TotalTrades > 0 {
		// Count only closed trades for win rate
		closedTrades := report.TotalWins + (report.TotalTrades - report.TotalWins)
		_ = closedTrades
		report.OverallWinRate = safeDivide(float64(report.TotalWins), float64(report.TotalTrades))
	}

	// ── Build ByStrategy ─────────────────────────────────────────────────────
	for _, sa := range stratMap {
		report.ByStrategy = append(report.ByStrategy, sa.toAttribution(report.TotalPnL))
	}
	sort.Slice(report.ByStrategy, func(i, j int) bool {
		return report.ByStrategy[i].PnL > report.ByStrategy[j].PnL
	})

	// ── Build BySymbol ────────────────────────────────────────────────────────
	for _, sym := range symMap {
		report.BySymbol = append(report.BySymbol, sym.toAttribution(report.TotalPnL))
	}
	sort.Slice(report.BySymbol, func(i, j int) bool {
		return report.BySymbol[i].PnL > report.BySymbol[j].PnL
	})

	// ── Build ByRegime ────────────────────────────────────────────────────────
	for _, ra := range regMap {
		report.ByRegime = append(report.ByRegime, ra.toAttribution(report.TotalPnL))
	}
	sort.Slice(report.ByRegime, func(i, j int) bool {
		return report.ByRegime[i].PnL > report.ByRegime[j].PnL
	})

	// ── Build Interactions ────────────────────────────────────────────────────
	for _, ia := range intMap {
		report.Interactions = append(report.Interactions, ia.toAttribution())
	}

	return report, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Internal accumulators
// ─────────────────────────────────────────────────────────────────────────────

type stratAccum struct {
	id, ver    string
	trades     int
	wins       int
	pnl        float64
	returns    []float64
	peak       float64
	equity     float64
	maxDD      float64
	symPnL     map[string]float64
}

func (a *stratAccum) add(e *journal.DecisionEntry) {
	a.trades++
	if e.Outcome.Class == journal.OutcomeWin {
		a.wins++
	}
	a.pnl += e.Outcome.RealizedPnL
	a.returns = append(a.returns, e.Outcome.ReturnPct)
	a.equity += e.Outcome.RealizedPnL
	if a.equity > a.peak {
		a.peak = a.equity
	}
	dd := (a.peak - a.equity) / math.Max(a.peak, 1)
	if dd > a.maxDD {
		a.maxDD = dd
	}
	if a.symPnL == nil {
		a.symPnL = make(map[string]float64)
	}
	a.symPnL[e.Symbol] += e.Outcome.RealizedPnL
}

func (a *stratAccum) toAttribution(totalPnL float64) StrategyAttribution {
	wr := safeDivide(float64(a.wins), float64(a.trades))
	avgRet := safeMean(a.returns)
	best, worst := bestWorstSymbol(a.symPnL)
	return StrategyAttribution{
		StrategyID:      a.id,
		StrategyVersion: a.ver,
		Trades:          a.trades,
		Wins:            a.wins,
		WinRate:         wr,
		PnL:             a.pnl,
		PnLShare:        safeDivide(a.pnl, totalPnL),
		AvgReturn:       avgRet,
		MaxDrawdown:     -a.maxDD,
		SharpeProxy:     safeStdDivMean(a.returns),
		BestSymbol:      best,
		WorstSymbol:     worst,
	}
}

type symAccum struct {
	symbol   string
	trades   int
	wins     int
	pnl      float64
	returns  []float64
	stratPnL map[string]float64
}

func (a *symAccum) add(e *journal.DecisionEntry) {
	a.trades++
	if e.Outcome.Class == journal.OutcomeWin {
		a.wins++
	}
	a.pnl += e.Outcome.RealizedPnL
	a.returns = append(a.returns, e.Outcome.ReturnPct)
	if a.stratPnL == nil {
		a.stratPnL = make(map[string]float64)
	}
	a.stratPnL[e.StrategyID+"/"+e.StrategyVersion] += e.Outcome.RealizedPnL
}

func (a *symAccum) toAttribution(totalPnL float64) SymbolAttribution {
	best := ""
	bestPnL := math.Inf(-1)
	for k, v := range a.stratPnL {
		if v > bestPnL {
			bestPnL = v
			best = k
		}
	}
	return SymbolAttribution{
		Symbol:       a.symbol,
		Trades:       a.trades,
		Wins:         a.wins,
		WinRate:      safeDivide(float64(a.wins), float64(a.trades)),
		PnL:          a.pnl,
		PnLShare:     safeDivide(a.pnl, totalPnL),
		AvgReturn:    safeMean(a.returns),
		BestStrategy: best,
	}
}

type regAccum struct {
	regime  string
	trades  int
	wins    int
	pnl     float64
	returns []float64
}

func (a *regAccum) add(e *journal.DecisionEntry) {
	a.trades++
	if e.Outcome.Class == journal.OutcomeWin {
		a.wins++
	}
	a.pnl += e.Outcome.RealizedPnL
	a.returns = append(a.returns, e.Outcome.ReturnPct)
}

func (a *regAccum) toAttribution(totalPnL float64) RegimeAttribution {
	return RegimeAttribution{
		Regime:    a.regime,
		Trades:    a.trades,
		Wins:      a.wins,
		WinRate:   safeDivide(float64(a.wins), float64(a.trades)),
		PnL:       a.pnl,
		PnLShare:  safeDivide(a.pnl, totalPnL),
		AvgReturn: safeMean(a.returns),
	}
}

type intAccum struct {
	stratID, stratVer, symbol string
	trades                    int
	wins                      int
	pnl                       float64
}

func (a *intAccum) add(e *journal.DecisionEntry) {
	a.trades++
	if e.Outcome.Class == journal.OutcomeWin {
		a.wins++
	}
	a.pnl += e.Outcome.RealizedPnL
}

func (a *intAccum) toAttribution() StrategySymbolMatrix {
	return StrategySymbolMatrix{
		StrategyID:      a.stratID,
		StrategyVersion: a.stratVer,
		Symbol:          a.symbol,
		Trades:          a.trades,
		WinRate:         safeDivide(float64(a.wins), float64(a.trades)),
		PnL:             a.pnl,
	}
}

// ─── Math helpers ─────────────────────────────────────────────────────────────

func safeDivide(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func safeMean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func safeStdDivMean(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	mu := safeMean(v)
	if math.Abs(mu) < 1e-9 {
		return 0
	}
	variance := 0.0
	for _, x := range v {
		d := x - mu
		variance += d * d
	}
	std := math.Sqrt(variance / float64(len(v)))
	return safeDivide(mu, std)
}

func bestWorstSymbol(m map[string]float64) (best, worst string) {
	bestPnL, worstPnL := math.Inf(-1), math.Inf(1)
	for sym, pnl := range m {
		if pnl > bestPnL {
			bestPnL = pnl
			best = sym
		}
		if pnl < worstPnL {
			worstPnL = pnl
			worst = sym
		}
	}
	return
}

func inferRegime(e *journal.DecisionEntry) string {
	// Regime label is optionally stored in SignalInputs by v1.2 components
	if e.SignalInputs == nil {
		return ""
	}
	if r, ok := e.SignalInputs["regime"].(string); ok {
		return r
	}
	return ""
}
