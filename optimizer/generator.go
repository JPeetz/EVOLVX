// Package optimizer – candidate generator.
//
// GenerateCandidates produces a bounded set of mutated parameter sets from a
// parent StrategyRecord.  It does NOT run any simulations — it only creates
// the parameter variations.  Each variation is one axis of the search space.
package optimizer

import (
	"fmt"
	"time"

	"github.com/NoFxAiOS/nofx/registry"
	"github.com/google/uuid"
)

// GenerateCandidates produces up to maxCandidates mutated variants of parent.
// The mutations are deterministic and cover a predefined grid so that the
// search is reproducible.
func GenerateCandidates(parent *registry.StrategyRecord, maxCandidates int) []Candidate {
	mutations := allMutations(parent.Parameters)
	if maxCandidates > 0 && len(mutations) > maxCandidates {
		mutations = mutations[:maxCandidates]
	}

	out := make([]Candidate, 0, len(mutations))
	for _, m := range mutations {
		out = append(out, Candidate{
			CandidateID:   uuid.NewString(),
			ParentID:      parent.ID,
			ParentVersion: parent.Version,
			Parameters:    m.params,
			MutationDesc:  m.desc,
			CreatedAt:     time.Now(),
		})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Mutation grid
// ─────────────────────────────────────────────────────────────────────────────

type mutation struct {
	params registry.Parameters
	desc   string
}

func allMutations(base registry.Parameters) []mutation {
	var out []mutation

	// RSI period variants
	for _, period := range []int{7, 9, 14, 21} {
		if containsInt(base.RSIPeriods, period) {
			continue
		}
		p := base
		p.RSIPeriods = []int{period}
		out = append(out, mutation{
			params: p,
			desc:   fmt.Sprintf("rsi_period=%d (was %v)", period, base.RSIPeriods),
		})
	}

	// EMA period variants
	for _, period := range []int{10, 20, 50, 100} {
		if containsInt(base.EMAPeriods, period) {
			continue
		}
		p := base
		p.EMAPeriods = []int{period}
		out = append(out, mutation{
			params: p,
			desc:   fmt.Sprintf("ema_period=%d (was %v)", period, base.EMAPeriods),
		})
	}

	// Leverage variants (altcoin)
	for _, lev := range []int{2, 3, 5, 7, 10} {
		if lev == base.AltcoinMaxLeverage {
			continue
		}
		p := base
		p.AltcoinMaxLeverage = lev
		out = append(out, mutation{
			params: p,
			desc:   fmt.Sprintf("altcoin_leverage=%d (was %d)", lev, base.AltcoinMaxLeverage),
		})
	}

	// Min confidence variants
	for _, conf := range []int{65, 70, 75, 80, 85} {
		if conf == base.MinConfidence {
			continue
		}
		p := base
		p.MinConfidence = conf
		out = append(out, mutation{
			params: p,
			desc:   fmt.Sprintf("min_confidence=%d (was %d)", conf, base.MinConfidence),
		})
	}

	// Max margin usage variants
	for _, margin := range []float64{0.5, 0.6, 0.7, 0.8, 0.9} {
		if abs(margin-base.MaxMarginUsage) < 0.01 {
			continue
		}
		p := base
		p.MaxMarginUsage = margin
		out = append(out, mutation{
			params: p,
			desc:   fmt.Sprintf("max_margin_usage=%.0f%% (was %.0f%%)", margin*100, base.MaxMarginUsage*100),
		})
	}

	// Trading mode variants
	for _, mode := range []string{"aggressive", "conservative", "scalping"} {
		if mode == base.TradingMode {
			continue
		}
		p := base
		p.TradingMode = mode
		out = append(out, mutation{
			params: p,
			desc:   fmt.Sprintf("trading_mode=%s (was %s)", mode, base.TradingMode),
		})
	}

	// Max positions variants
	for _, mp := range []int{2, 3, 4, 5} {
		if mp == base.MaxPositions {
			continue
		}
		p := base
		p.MaxPositions = mp
		out = append(out, mutation{
			params: p,
			desc:   fmt.Sprintf("max_positions=%d (was %d)", mp, base.MaxPositions),
		})
	}

	return out
}

func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
