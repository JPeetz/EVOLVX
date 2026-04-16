// Package registry defines versioned strategy records.
//
// A strategy is an immutable, serialisable artifact.  Every change produces a
// new version; the old version is never overwritten.  This makes backtests and
// paper runs reproducible: they always reference a specific version.
package registry

import (
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Strategy status lifecycle
// ─────────────────────────────────────────────────────────────────────────────

// StrategyStatus is the promotion/demotion state of a strategy version.
type StrategyStatus string

const (
	StatusDraft      StrategyStatus = "draft"      // being developed
	StatusPaper      StrategyStatus = "paper"       // running in paper mode
	StatusApproved   StrategyStatus = "approved"    // human-approved for live
	StatusDeprecated StrategyStatus = "deprecated"  // replaced by newer version
	StatusDisabled   StrategyStatus = "disabled"    // turned off explicitly
)

// ─────────────────────────────────────────────────────────────────────────────
// StrategyRecord – the durable artifact
// ─────────────────────────────────────────────────────────────────────────────

// StrategyRecord is the complete, serialisable description of one strategy
// version.  Once written, it is immutable — changing anything produces a new
// record with a bumped Version.
type StrategyRecord struct {
	// Identity
	ID        string    `json:"id"`         // UUID, stable across versions
	Name      string    `json:"name"`       // human-readable
	Version   string    `json:"version"`    // semver e.g. "1.0.0"
	CreatedAt time.Time `json:"created_at"`
	Author    string    `json:"author"`

	// Lineage
	ParentID      string `json:"parent_id,omitempty"`       // strategy ID of parent (for clones)
	ParentVersion string `json:"parent_version,omitempty"`  // exact parent version

	// Status
	Status          StrategyStatus `json:"status"`
	StatusChangedAt time.Time      `json:"status_changed_at"`
	StatusChangedBy string         `json:"status_changed_by"`

	// Parameters (JSON-serialisable, fed into the prompt builder)
	Parameters Parameters `json:"parameters"`

	// Metadata
	CompatibleMarkets    []string `json:"compatible_markets"`    // "crypto", "equities"
	CompatibleTimeframes []string `json:"compatible_timeframes"` // "5m", "1h", etc.

	// Performance history – populated after each evaluated run
	Performance []PerformanceSummary `json:"performance,omitempty"`

	// Raw config – stores the StrategyConfig JSON from the existing store
	// so migration can round-trip without data loss
	RawConfig string `json:"raw_config,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Parameters
// ─────────────────────────────────────────────────────────────────────────────

// Parameters mirrors the fields that the pipeline and prompt builder need.
// This is a strict, typed subset of the existing StrategyConfig.
type Parameters struct {
	// Coin source
	CoinSourceType  string   `json:"coin_source_type"`   // "static","coinpool","oi_top","mixed"
	StaticCoins     []string `json:"static_coins"`
	UseCoinPool     bool     `json:"use_coin_pool"`
	UseOITop        bool     `json:"use_oi_top"`
	CoinPoolLimit   int      `json:"coin_pool_limit"`

	// Indicators
	EnableEMA         bool  `json:"enable_ema"`
	EMAPeriods        []int `json:"ema_periods"`
	EnableMACD        bool  `json:"enable_macd"`
	EnableRSI         bool  `json:"enable_rsi"`
	RSIPeriods        []int `json:"rsi_periods"`
	EnableATR         bool  `json:"enable_atr"`
	ATRPeriods        []int `json:"atr_periods"`
	EnableVolume      bool  `json:"enable_volume"`
	EnableOI          bool  `json:"enable_oi"`
	EnableFundingRate bool  `json:"enable_funding_rate"`
	EnableQuantData   bool  `json:"enable_quant_data"`

	// Kline config
	PrimaryTimeframe    string   `json:"primary_timeframe"`    // "5m"
	SelectedTimeframes  []string `json:"selected_timeframes"`  // ["5m","15m","1h","4h"]
	PrimaryCount        int      `json:"primary_count"`        // 30

	// Risk controls
	MaxPositions                   int     `json:"max_positions"`
	BTCETHMaxLeverage              int     `json:"btceth_max_leverage"`
	AltcoinMaxLeverage             int     `json:"altcoin_max_leverage"`
	BTCETHMaxPositionValueRatio    float64 `json:"btceth_max_position_value_ratio"`
	AltcoinMaxPositionValueRatio   float64 `json:"altcoin_max_position_value_ratio"`
	MaxMarginUsage                 float64 `json:"max_margin_usage"`
	MinPositionSize                float64 `json:"min_position_size"`
	MinRiskRewardRatio             float64 `json:"min_risk_reward_ratio"`
	MinConfidence                  int     `json:"min_confidence"`

	// Trading mode
	TradingMode string `json:"trading_mode"` // "aggressive","conservative","scalping"

	// Prompt overrides
	RoleDefinition   string `json:"role_definition,omitempty"`
	TradingFrequency string `json:"trading_frequency,omitempty"`
	EntryStandards   string `json:"entry_standards,omitempty"`
	DecisionProcess  string `json:"decision_process,omitempty"`
	CustomPrompt     string `json:"custom_prompt,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// PerformanceSummary
// ─────────────────────────────────────────────────────────────────────────────

// RunType classifies what kind of evaluation produced the metrics.
type RunType string

const (
	RunBacktest RunType = "backtest"
	RunPaper    RunType = "paper"
	RunLive     RunType = "live"
)

// PerformanceSummary records the outcome of one evaluated run tied to a
// specific strategy version.
type PerformanceSummary struct {
	RunID           string    `json:"run_id"`
	RunType         RunType   `json:"run_type"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	NetReturn       float64   `json:"net_return"`        // fraction e.g. 0.12 = 12%
	MaxDrawdown     float64   `json:"max_drawdown"`      // fraction e.g. -0.05
	SharpeRatio     float64   `json:"sharpe_ratio"`
	SortinoRatio    float64   `json:"sortino_ratio"`
	WinRate         float64   `json:"win_rate"`          // fraction
	ProfitFactor    float64   `json:"profit_factor"`
	TotalTrades     int       `json:"total_trades"`
	// Walk-forward split info
	TrainPeriod     string    `json:"train_period,omitempty"`
	ValidationPeriod string   `json:"validation_period,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Strategy lineage node (for optimizer)
// ─────────────────────────────────────────────────────────────────────────────

// LineageNode tracks one node in the parent→child strategy evolution graph.
type LineageNode struct {
	StrategyID      string    `json:"strategy_id"`
	Version         string    `json:"version"`
	ParentID        string    `json:"parent_id"`
	ParentVersion   string    `json:"parent_version"`
	MutationSummary string    `json:"mutation_summary"` // what changed
	CreatedAt       time.Time `json:"created_at"`
	EvalScore       float64   `json:"eval_score"`
	Promoted        bool      `json:"promoted"`
	PromotedAt      *time.Time `json:"promoted_at,omitempty"`
}
