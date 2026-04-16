// Package trader – pipeline integration shim.
//
// This file shows the exact changes needed in auto_trader.go to route all
// execution through the unified pipeline.  It is written as a new file
// (auto_trader_pipeline.go) that the existing auto_trader.go can call.
// The goal is a MINIMAL change to auto_trader.go: replace the body of
// runCycle() with a call to PipelineCycle().
//
// No logic is removed from auto_trader.go in the first refactor pass —
// we ADD the pipeline path and disable the legacy path with a feature flag.
package trader

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/NoFxAiOS/nofx/engine/adapters"
	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/engine/feeds"
	"github.com/NoFxAiOS/nofx/engine/pipeline"
	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// PipelineRunner – thin wrapper the AutoTrader delegates to
// ─────────────────────────────────────────────────────────────────────────────

// PipelineRunner holds all dependencies needed to run one trading cycle
// through the unified pipeline.  The AutoTrader creates one PipelineRunner
// per trader session and calls RunCycle() on each interval.
type PipelineRunner struct {
	strategy  *registry.StrategyRecord
	mode      core.Mode
	adapter   adapters.ExecutionAdapter
	evaluator pipeline.StrategyEvaluator
	risk      *pipeline.StandardRiskChecker
	metrics   *pipeline.RunningMetrics
	logger    adapters.EventLogger
	journal   *pipeline.JournalRecorder
	feed      *feeds.ChannelFeed // injected by the live event loop
	eventCh   chan *core.MarketEvent
}

// NewPipelineRunnerForLive creates a PipelineRunner wired for live trading.
//
//   at:       the existing AutoTrader (provides exchange client)
//   strategy: the versioned strategy record from the registry
//   j:        the decision journal service
func NewPipelineRunnerForLive(
	exchangeClient adapters.ExchangeClient,
	strategy *registry.StrategyRecord,
	j *journal.Service,
	legacyEngine pipeline.LegacyDecisionEngine,
	logPath string,
) (*PipelineRunner, error) {

	mode := core.ModeLive

	liveAdapter := adapters.NewLiveAdapter(exchangeClient)

	evalWrapper := pipeline.NewAIStrategyEvaluator(
		strategy.ID,
		strategy.Version,
		strategy.Parameters,
		legacyEngine,
		j,
	)

	riskChecker := pipeline.NewStandardRiskChecker(strategy.Parameters)

	equity, _, _ := liveAdapter.GetBalance(context.Background())
	metricsCollector := pipeline.NewRunningMetrics(equity, strategy.ID, strategy.Version, mode)

	var logger adapters.EventLogger
	if logPath != "" {
		var err error
		logger, err = pipeline.NewSQLiteEventLogger(logPath, 200)
		if err != nil {
			return nil, fmt.Errorf("pipeline runner: create logger: %w", err)
		}
	}

	eventCh := make(chan *core.MarketEvent, 64)
	feed := feeds.NewChannelFeed(eventCh)

	return &PipelineRunner{
		strategy:  strategy,
		mode:      mode,
		adapter:   liveAdapter,
		evaluator: evalWrapper,
		risk:      riskChecker,
		metrics:   metricsCollector,
		logger:    logger,
		journal:   pipeline.NewJournalRecorder(j),
		feed:      feed,
		eventCh:   eventCh,
	}, nil
}

// NewPipelineRunnerForPaper creates a PipelineRunner for paper mode.
// Paper mode uses the simulated adapter (no real orders) but receives
// live market data.
func NewPipelineRunnerForPaper(
	strategy *registry.StrategyRecord,
	initialEquity float64,
	j *journal.Service,
	legacyEngine pipeline.LegacyDecisionEngine,
	logPath string,
) (*PipelineRunner, error) {

	mode := core.ModePaper

	simAdapter := adapters.NewSimulatedAdapter(initialEquity, mode, core.DefaultFillModel())

	evalWrapper := pipeline.NewAIStrategyEvaluator(
		strategy.ID, strategy.Version, strategy.Parameters, legacyEngine, j,
	)
	riskChecker := pipeline.NewStandardRiskChecker(strategy.Parameters)
	metricsCollector := pipeline.NewRunningMetrics(initialEquity, strategy.ID, strategy.Version, mode)

	var logger adapters.EventLogger
	if logPath != "" {
		var err error
		logger, err = pipeline.NewSQLiteEventLogger(logPath, 200)
		if err != nil {
			return nil, fmt.Errorf("pipeline runner: create logger: %w", err)
		}
	}

	eventCh := make(chan *core.MarketEvent, 64)
	feed := feeds.NewChannelFeed(eventCh)

	return &PipelineRunner{
		strategy:  strategy,
		mode:      mode,
		adapter:   simAdapter,
		evaluator: evalWrapper,
		risk:      riskChecker,
		metrics:   metricsCollector,
		logger:    logger,
		journal:   pipeline.NewJournalRecorder(j),
		feed:      feed,
		eventCh:   eventCh,
	}, nil
}

// Run starts the pipeline loop.  It blocks until ctx is cancelled.
// Call SendMarketEvent() from the existing ticker to deliver data.
func (r *PipelineRunner) Run(ctx context.Context) error {
	p, err := pipeline.New(pipeline.Config{
		Mode:                r.mode,
		StrategyID:          r.strategy.ID,
		StrategyVersion:     r.strategy.Version,
		Feed:                r.feed,
		Adapter:             r.adapter,
		Evaluator:           r.evaluator,
		Risk:                r.risk,
		Metrics:             r.metrics,
		Logger:              r.logger,
		FillPollInterval:    500 * time.Millisecond,
		FillPollMaxAttempts: 10,
	})
	if err != nil {
		return err
	}
	return p.Run(ctx)
}

// SendMarketEvent is called by the existing market data ticker to inject a
// new bar into the pipeline.  This replaces the direct call to
// decision.StrategyEngine.GetFullDecisionWithStrategy().
func (r *PipelineRunner) SendMarketEvent(event *core.MarketEvent) {
	select {
	case r.eventCh <- event:
	default:
		log.Printf("pipeline runner: event channel full, dropping event for %s", event.Symbol)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Backtest entry point
// ─────────────────────────────────────────────────────────────────────────────

// RunBacktest executes a historical backtest for a strategy version.
// This replaces the existing backtest package's entry point.
//
// dbPath:         path to the nofx klines database
// strategy:       the specific version to test
// initialEquity:  starting USDT balance
// symbol:         trading pair e.g. "BTCUSDT"
// timeframe:      bar timeframe e.g. "5m"
// from/to:        date range
// logPath:        where to write the event log (empty = no log)
func RunBacktest(
	ctx context.Context,
	dbPath string,
	strategy *registry.StrategyRecord,
	legacyEngine pipeline.LegacyDecisionEngine,
	j *journal.Service,
	initialEquity float64,
	symbol, timeframe string,
	from, to time.Time,
	logPath string,
) (*pipeline.RunningMetrics, error) {

	feed, err := feeds.NewSQLiteHistoricalFeed(dbPath, symbol, timeframe, from, to)
	if err != nil {
		return nil, fmt.Errorf("backtest: create feed: %w", err)
	}

	simAdapter := adapters.NewSimulatedAdapter(initialEquity, core.ModeBacktest, core.DefaultFillModel())

	evalWrapper := pipeline.NewAIStrategyEvaluator(
		strategy.ID, strategy.Version, strategy.Parameters, legacyEngine, j,
	)
	riskChecker := pipeline.NewStandardRiskChecker(strategy.Parameters)
	metricsCollector := pipeline.NewRunningMetrics(initialEquity, strategy.ID, strategy.Version, core.ModeBacktest)

	var logger adapters.EventLogger
	if logPath != "" {
		logger, err = pipeline.NewSQLiteEventLogger(logPath, 500)
		if err != nil {
			return nil, fmt.Errorf("backtest: create logger: %w", err)
		}
	}

	p, err := pipeline.New(pipeline.Config{
		Mode:            core.ModeBacktest,
		StrategyID:      strategy.ID,
		StrategyVersion: strategy.Version,
		Feed:            feed,
		Adapter:         simAdapter,
		Evaluator:       evalWrapper,
		Risk:            riskChecker,
		Metrics:         metricsCollector,
		Logger:          logger,
	})
	if err != nil {
		return nil, err
	}

	if err := p.Run(ctx); err != nil && err != context.Canceled {
		return nil, fmt.Errorf("backtest: run: %w", err)
	}

	return metricsCollector, nil
}
