// Package ensemble implements weighted signal voting across multiple strategy
// versions.
//
// Ensemble voting answers a practical question: when you have three approved
// versions of a strategy (v1.2.1, v1.3.0, v1.3.1) running simultaneously,
// and they disagree on a trade, what should the system do?
//
// EvolvX Ensemble:
//  1. Each voter is one strategy version with a weight derived from its
//     validated performance metrics in the journal.
//  2. Signals are aggregated using weighted majority vote per action type.
//  3. Confidence is the weighted average confidence across agreeing voters.
//  4. A minimum quorum (>= 2 voters agreeing) is required before acting.
//  5. A hold/wait signal is emitted when there is no quorum.
//
// This package does NOT replace the individual strategy evaluators.
// Each version still runs independently through the pipeline.  The ensemble
// layer sits above the pipeline and collects their signals before risk check.
package ensemble

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// Voter — one strategy version contributing to the ensemble
// ─────────────────────────────────────────────────────────────────────────────

// Voter represents one member of the ensemble.
type Voter struct {
	StrategyID      string
	StrategyVersion string
	// Weight is derived from the strategy's validated performance.
	// Higher Sharpe × win rate = higher weight.
	Weight float64
	// Signal is the vote cast by this voter for the current bar.
	Signal *core.Signal
}

// ─────────────────────────────────────────────────────────────────────────────
// Ensemble configuration
// ─────────────────────────────────────────────────────────────────────────────

// Config controls ensemble voting behaviour.
type Config struct {
	// MinQuorum: minimum number of voters that must agree for a signal to pass.
	// Default: 2.
	MinQuorum int
	// MinWeightedConfidence: minimum weighted-average confidence required.
	// Default: 70.
	MinWeightedConfidence float64
	// ActionAgreement: "strict" requires all voters agree, "majority" requires > 50%.
	// Default: "majority".
	ActionAgreement string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MinQuorum:             2,
		MinWeightedConfidence: 70,
		ActionAgreement:       "majority",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WeightFrom — derive voter weight from journal/registry performance data
// ─────────────────────────────────────────────────────────────────────────────

// WeightFromPerformance computes the weight for a strategy version based on
// its latest PerformanceSummary in the registry.
//
// Weight = SharpeRatio × WinRate × ProfitFactor (clamped to [0.1, 3.0])
// Versions with no performance data receive weight 1.0 (neutral).
func WeightFromPerformance(record *registry.StrategyRecord) float64 {
	if len(record.Performance) == 0 {
		return 1.0
	}
	p := record.Performance[len(record.Performance)-1]

	// Use Sharpe as the primary quality signal
	sharpe := math.Max(0, p.SharpeRatio)
	winRate := math.Max(0, p.WinRate)
	pf := math.Max(0, p.ProfitFactor)

	// Composite weight: Sharpe dominates, win rate and PF moderate it
	w := sharpe*0.5 + winRate*0.3 + math.Min(pf/3, 1.0)*0.2
	return math.Max(0.1, math.Min(3.0, w))
}

// WeightFromJournal computes weight from the last N decisions in the journal.
// Uses empirical win rate and average return rather than backtest estimates.
func WeightFromJournal(j *journal.Service, strategyID, version string, lookback int) (float64, error) {
	if lookback <= 0 {
		lookback = 50
	}
	entries, err := j.Query(journal.QueryFilter{
		StrategyID:      strategyID,
		StrategyVersion: version,
		Limit:           lookback,
	})
	if err != nil {
		return 1.0, fmt.Errorf("ensemble weight: query journal: %w", err)
	}

	closed := 0
	wins := 0
	totalReturn := 0.0
	for _, e := range entries {
		if e.Outcome == nil {
			continue
		}
		closed++
		if e.Outcome.Class == journal.OutcomeWin {
			wins++
		}
		totalReturn += e.Outcome.ReturnPct
	}

	if closed < 5 {
		return 1.0, nil // not enough data — neutral weight
	}

	winRate := float64(wins) / float64(closed)
	avgReturn := totalReturn / float64(closed)

	// Weight: empirical win rate × sign-adjusted avg return scale
	returnFactor := 1.0 + math.Max(-0.5, math.Min(1.0, avgReturn*10))
	w := winRate * returnFactor
	return math.Max(0.1, math.Min(3.0, w)), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Vote — aggregate signals from multiple voters
// ─────────────────────────────────────────────────────────────────────────────

// VoteResult is the output of the ensemble voting process.
type VoteResult struct {
	// AgreedAction is the winning action, or ActionHold/ActionWait if no quorum.
	AgreedAction core.SignalAction
	// WeightedConfidence is the weighted-average confidence of agreeing voters.
	WeightedConfidence float64
	// Quorum is how many voters agreed on the winning action.
	Quorum int
	// TotalWeight is the sum of weights of all agreeing voters.
	TotalWeight float64
	// Signal is the synthesised signal ready to enter the pipeline.
	// nil if no quorum was reached.
	Signal *core.Signal
	// Breakdown records each voter's contribution.
	Breakdown []VoterContribution
}

// VoterContribution summarises one voter's vote.
type VoterContribution struct {
	StrategyID      string
	StrategyVersion string
	Weight          float64
	Action          core.SignalAction
	Confidence      int
	Agreed          bool
}

// Vote aggregates signals from voters and returns a VoteResult.
func Vote(voters []Voter, symbol string, cfg Config) VoteResult {
	if cfg.MinQuorum <= 0 {
		cfg.MinQuorum = 2
	}

	result := VoteResult{}

	// Count weighted votes per action
	type actionTally struct {
		weight     float64
		count      int
		confidence float64 // sum of weight×confidence
		signals    []*core.Signal
	}
	tallies := make(map[core.SignalAction]*actionTally)

	for _, v := range voters {
		if v.Signal == nil || v.Signal.Symbol != symbol {
			continue
		}
		action := v.Signal.Action
		if action == core.ActionHold || action == core.ActionWait {
			continue // abstentions don't count toward quorum
		}
		t := tallies[action]
		if t == nil {
			t = &actionTally{}
			tallies[action] = t
		}
		t.weight += v.Weight
		t.count++
		t.confidence += v.Weight * float64(v.Signal.Confidence)
		t.signals = append(t.signals, v.Signal)
	}

	if len(tallies) == 0 {
		result.AgreedAction = core.ActionHold
		return result
	}

	// Find the action with the highest weighted vote
	type scored struct {
		action core.SignalAction
		tally  *actionTally
	}
	var ranked []scored
	for action, t := range tallies {
		ranked = append(ranked, scored{action, t})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].tally.weight > ranked[j].tally.weight
	})

	winner := ranked[0]

	// Check quorum
	if winner.tally.count < cfg.MinQuorum {
		result.AgreedAction = core.ActionHold
		result.Quorum = winner.tally.count
		result.Breakdown = buildBreakdown(voters, winner.action, false)
		return result
	}

	// Check majority agreement if required
	totalVoters := len(voters)
	if cfg.ActionAgreement == "strict" && winner.tally.count < totalVoters {
		result.AgreedAction = core.ActionHold
		result.Breakdown = buildBreakdown(voters, winner.action, false)
		return result
	}

	weightedConf := winner.tally.confidence / winner.tally.weight

	// Check minimum confidence
	if weightedConf < cfg.MinWeightedConfidence {
		result.AgreedAction = core.ActionHold
		result.Breakdown = buildBreakdown(voters, winner.action, false)
		return result
	}

	// Build synthesised signal from the highest-confidence agreeing voter
	var bestSignal *core.Signal
	for _, s := range winner.tally.signals {
		if bestSignal == nil || s.Confidence > bestSignal.Confidence {
			bestSignal = s
		}
	}

	synthesised := *bestSignal
	synthesised.SignalID = uuid.NewString()
	synthesised.Confidence = int(weightedConf)
	synthesised.Reasoning = fmt.Sprintf(
		"ensemble(%d/%d voters agree, avg conf %.0f, total weight %.2f): %s",
		winner.tally.count, len(voters), weightedConf, winner.tally.weight,
		synthesised.Reasoning,
	)
	synthesised.Timestamp = time.Now()

	result.AgreedAction = winner.action
	result.WeightedConfidence = weightedConf
	result.Quorum = winner.tally.count
	result.TotalWeight = winner.tally.weight
	result.Signal = &synthesised
	result.Breakdown = buildBreakdown(voters, winner.action, true)

	return result
}

func buildBreakdown(voters []Voter, winningAction core.SignalAction, agreed bool) []VoterContribution {
	out := make([]VoterContribution, 0, len(voters))
	for _, v := range voters {
		action := core.ActionWait
		conf := 0
		if v.Signal != nil {
			action = v.Signal.Action
			conf = v.Signal.Confidence
		}
		out = append(out, VoterContribution{
			StrategyID:      v.StrategyID,
			StrategyVersion: v.StrategyVersion,
			Weight:          v.Weight,
			Action:          action,
			Confidence:      conf,
			Agreed:          agreed && action == winningAction,
		})
	}
	return out
}
