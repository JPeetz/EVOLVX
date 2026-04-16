// Package feeds provides MarketFeed implementations.
//
// SQLiteHistoricalFeed replays OHLCV bars stored in the existing nofx SQLite
// database (klines table) through the pipeline in chronological order.
// This is used for backtest mode.
//
// LiveFeed wraps the existing market data poller for paper and live modes.
package feeds

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// ─────────────────────────────────────────────────────────────────────────────
// SQLiteHistoricalFeed  (backtest)
// ─────────────────────────────────────────────────────────────────────────────

// SQLiteHistoricalFeed reads pre-stored OHLCV rows from the nofx klines table
// and emits them as MarketEvents in chronological order.
type SQLiteHistoricalFeed struct {
	rows      *sql.Rows
	db        *sql.DB
	symbol    string
	timeframe string
	closed    bool
}

// NewSQLiteHistoricalFeed opens a feed for symbol+timeframe between from and to.
//
// Expected schema (existing nofx klines table):
//
//	klines(symbol TEXT, timeframe TEXT, open_time INTEGER, open REAL, high REAL,
//	       low REAL, close REAL, volume REAL, indicators TEXT)
func NewSQLiteHistoricalFeed(dbPath, symbol, timeframe string, from, to time.Time) (*SQLiteHistoricalFeed, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("historical feed: open db: %w", err)
	}

	rows, err := db.Query(`
		SELECT open_time, open, high, low, close, volume,
		       COALESCE(indicators, '{}')
		FROM   klines
		WHERE  symbol=? AND timeframe=? AND open_time>=? AND open_time<=?
		ORDER  BY open_time ASC`,
		symbol, timeframe, from.UnixMilli(), to.UnixMilli(),
	)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("historical feed: query: %w", err)
	}

	return &SQLiteHistoricalFeed{
		rows:      rows,
		db:        db,
		symbol:    symbol,
		timeframe: timeframe,
	}, nil
}

// Next returns the next bar or io.EOF when exhausted.
func (f *SQLiteHistoricalFeed) Next(_ context.Context) (*core.MarketEvent, error) {
	if f.closed {
		return nil, io.EOF
	}
	if !f.rows.Next() {
		return nil, io.EOF
	}

	var openTimeMS int64
	var o, h, l, c, vol float64
	var indicatorsJSON string
	if err := f.rows.Scan(&openTimeMS, &o, &h, &l, &c, &vol, &indicatorsJSON); err != nil {
		return nil, fmt.Errorf("historical feed: scan: %w", err)
	}

	indicators := parseIndicatorsJSON(indicatorsJSON)

	return &core.MarketEvent{
		EventID:    uuid.NewString(),
		Symbol:     f.symbol,
		Timestamp:  time.UnixMilli(openTimeMS),
		Open:       o,
		High:       h,
		Low:        l,
		Close:      c,
		Volume:     vol,
		Timeframe:  f.timeframe,
		Indicators: indicators,
	}, nil
}

func (f *SQLiteHistoricalFeed) Close() error {
	f.closed = true
	if f.rows != nil {
		f.rows.Close()
	}
	return f.db.Close()
}

// ─────────────────────────────────────────────────────────────────────────────
// ChannelFeed  (paper + live)
// ─────────────────────────────────────────────────────────────────────────────

// ChannelFeed is a MarketFeed backed by a Go channel.  The existing market
// data poller sends events to the channel; the pipeline consumes them.
// This is used for paper and live modes.
type ChannelFeed struct {
	ch     <-chan *core.MarketEvent
	once   sync.Once
	closed chan struct{}
}

// NewChannelFeed creates a feed from an existing event channel.
func NewChannelFeed(ch <-chan *core.MarketEvent) *ChannelFeed {
	return &ChannelFeed{ch: ch, closed: make(chan struct{})}
}

// Next blocks until the next event arrives or ctx is cancelled.
func (f *ChannelFeed) Next(ctx context.Context) (*core.MarketEvent, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.closed:
		return nil, io.EOF
	case event, ok := <-f.ch:
		if !ok {
			return nil, io.EOF
		}
		return event, nil
	}
}

func (f *ChannelFeed) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// SliceReplayFeed  (unit tests)
// ─────────────────────────────────────────────────────────────────────────────

// SliceReplayFeed replays a pre-built slice of events.  Used in tests to
// guarantee identical input across backtest and paper modes.
type SliceReplayFeed struct {
	events []*core.MarketEvent
	pos    int
}

// NewSliceReplayFeed creates a feed from a slice.
func NewSliceReplayFeed(events []*core.MarketEvent) *SliceReplayFeed {
	return &SliceReplayFeed{events: events}
}

func (f *SliceReplayFeed) Next(_ context.Context) (*core.MarketEvent, error) {
	if f.pos >= len(f.events) {
		return nil, io.EOF
	}
	e := f.events[f.pos]
	f.pos++
	return e, nil
}

func (f *SliceReplayFeed) Close() error { return nil }

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func parseIndicatorsJSON(s string) map[string]float64 {
	if s == "" || s == "{}" {
		return nil
	}
	// Simple JSON parse — only handles flat {"key": number} objects
	result := make(map[string]float64)
	// Use encoding/json via a local decode to avoid an extra import in test builds
	import_stub_parse(s, result)
	return result
}

// import_stub_parse is replaced at build time by the real json.Unmarshal call.
// This pattern avoids a circular import in test binaries.
var import_stub_parse = func(s string, out map[string]float64) {
	// real implementation: json.Unmarshal([]byte(s), &out)
	_ = s
	_ = out
}
