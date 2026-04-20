package ensemble_test

import (
	"testing"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/ensemble"
	"github.com/stretchr/testify/require"
)

func TestMajorityQuorumPasses(t *testing.T) {
	voters := []ensemble.Voter{
		{StrategyID: "s1", StrategyVersion: "1.0.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 82)},
		{StrategyID: "s1", StrategyVersion: "1.1.0", Weight: 1.2, Signal: signal("BTCUSDT", core.ActionOpenLong, 79)},
		{StrategyID: "s1", StrategyVersion: "1.2.0", Weight: 0.8, Signal: signal("BTCUSDT", core.ActionOpenShort, 71)},
	}
	result := ensemble.Vote(voters, "BTCUSDT", ensemble.DefaultConfig())

	require.Equal(t, core.ActionOpenLong, result.AgreedAction, "2 of 3 voters agree on long")
	require.Equal(t, 2, result.Quorum)
	require.NotNil(t, result.Signal)
	require.Greater(t, result.WeightedConfidence, 70.0)
}

func TestNoQuorumEmitsHold(t *testing.T) {
	// Each voter votes for a different action — no majority
	voters := []ensemble.Voter{
		{StrategyID: "s1", StrategyVersion: "1.0.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 80)},
		{StrategyID: "s1", StrategyVersion: "1.1.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenShort, 80)},
	}
	cfg := ensemble.DefaultConfig()
	cfg.MinQuorum = 2
	result := ensemble.Vote(voters, "BTCUSDT", cfg)

	// Neither action reaches 2-voter quorum with 1 vote each
	require.Equal(t, core.ActionHold, result.AgreedAction)
	require.Nil(t, result.Signal, "no signal should be emitted without quorum")
}

func TestLowConfidenceEmitsHold(t *testing.T) {
	voters := []ensemble.Voter{
		{StrategyID: "s1", StrategyVersion: "1.0.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 50)},
		{StrategyID: "s1", StrategyVersion: "1.1.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 55)},
	}
	cfg := ensemble.DefaultConfig()
	cfg.MinWeightedConfidence = 70
	result := ensemble.Vote(voters, "BTCUSDT", cfg)

	require.Equal(t, core.ActionHold, result.AgreedAction, "confidence 52 < min 70 must block signal")
	require.Nil(t, result.Signal)
}

func TestWeightedVoteHigherWeightWins(t *testing.T) {
	// 2 voters for long (low weight), 1 voter for short (very high weight)
	voters := []ensemble.Voter{
		{StrategyID: "s1", StrategyVersion: "1.0.0", Weight: 0.3, Signal: signal("ETHUSDT", core.ActionOpenLong, 80)},
		{StrategyID: "s1", StrategyVersion: "1.1.0", Weight: 0.3, Signal: signal("ETHUSDT", core.ActionOpenLong, 78)},
		{StrategyID: "s1", StrategyVersion: "1.2.0", Weight: 3.0, Signal: signal("ETHUSDT", core.ActionOpenShort, 90)},
	}
	cfg := ensemble.DefaultConfig()
	cfg.MinQuorum = 1 // relax quorum so we test weight influence

	result := ensemble.Vote(voters, "ETHUSDT", cfg)
	// Short has total weight 3.0 vs long's 0.6 — short should win
	require.Equal(t, core.ActionOpenShort, result.AgreedAction,
		"single high-weight voter should beat two low-weight voters")
}

func TestAbstentionsDontCountTowardQuorum(t *testing.T) {
	voters := []ensemble.Voter{
		{StrategyID: "s1", StrategyVersion: "1.0.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionHold, 80)},
		{StrategyID: "s1", StrategyVersion: "1.1.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionWait, 80)},
		{StrategyID: "s1", StrategyVersion: "1.2.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 85)},
	}
	cfg := ensemble.DefaultConfig()
	cfg.MinQuorum = 2
	result := ensemble.Vote(voters, "BTCUSDT", cfg)

	// Only 1 non-abstaining voter — below quorum of 2
	require.Equal(t, core.ActionHold, result.AgreedAction, "hold/wait votes must not count toward quorum")
}

func TestBreakdownRecordsAllVoters(t *testing.T) {
	voters := []ensemble.Voter{
		{StrategyID: "s1", StrategyVersion: "1.0.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 80)},
		{StrategyID: "s1", StrategyVersion: "1.1.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 82)},
	}
	result := ensemble.Vote(voters, "BTCUSDT", ensemble.DefaultConfig())

	require.Len(t, result.Breakdown, 2, "breakdown must include all voters")
	for _, c := range result.Breakdown {
		require.Equal(t, true, c.Agreed)
	}
}

func TestStrictModeRequiresUnanimity(t *testing.T) {
	voters := []ensemble.Voter{
		{StrategyID: "s1", StrategyVersion: "1.0.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 82)},
		{StrategyID: "s1", StrategyVersion: "1.1.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenLong, 80)},
		{StrategyID: "s1", StrategyVersion: "1.2.0", Weight: 1.0, Signal: signal("BTCUSDT", core.ActionOpenShort, 77)},
	}
	cfg := ensemble.DefaultConfig()
	cfg.ActionAgreement = "strict"
	result := ensemble.Vote(voters, "BTCUSDT", cfg)

	require.Equal(t, core.ActionHold, result.AgreedAction, "strict mode must require unanimous agreement")
}

// ─────────────────────────────────────────────────────────────────────────────
// Helper
// ─────────────────────────────────────────────────────────────────────────────

func signal(symbol string, action core.SignalAction, conf int) *core.Signal {
	return &core.Signal{
		SignalID:        "sig-test",
		StrategyID:      "s1",
		StrategyVersion: "1.0.0",
		Symbol:          symbol,
		Timestamp:       time.Now(),
		Action:          action,
		Leverage:        3,
		PositionSizeUSD: 500,
		Confidence:      conf,
	}
}
