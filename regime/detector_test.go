package regime_test

import (
	"math"
	"testing"
	"time"

	"github.com/NoFxAiOS/nofx/regime"
	"github.com/stretchr/testify/require"
)

func TestBullRegimeDetected(t *testing.T) {
	bars := makeTrendingBars(200, 10000, +0.002) // steady uptrend
	d := regime.New(regime.DefaultConfig())
	labeled := d.Classify(bars)

	// After warmup, majority should be bull
	warmup := regime.DefaultConfig().EMAPeriod
	bull := 0
	for _, lb := range labeled[warmup:] {
		if lb.Regime == regime.Bull {
			bull++
		}
	}
	pct := float64(bull) / float64(len(labeled)-warmup)
	require.Greater(t, pct, 0.5, "majority of bars in steady uptrend should be classified Bull, got %.1f%%", pct*100)
}

func TestBearRegimeDetected(t *testing.T) {
	bars := makeTrendingBars(200, 10000, -0.002) // steady downtrend
	d := regime.New(regime.DefaultConfig())
	labeled := d.Classify(bars)

	warmup := regime.DefaultConfig().EMAPeriod
	bear := 0
	for _, lb := range labeled[warmup:] {
		if lb.Regime == regime.Bear {
			bear++
		}
	}
	pct := float64(bear) / float64(len(labeled)-warmup)
	require.Greater(t, pct, 0.5, "majority of bars in steady downtrend should be Bear, got %.1f%%", pct*100)
}

func TestSidewaysRegimeDetected(t *testing.T) {
	bars := makeChoppyBars(200, 10000, 0.001) // oscillating around flat
	d := regime.New(regime.DefaultConfig())
	labeled := d.Classify(bars)

	warmup := regime.DefaultConfig().EMAPeriod
	sideways := 0
	for _, lb := range labeled[warmup:] {
		if lb.Regime == regime.Sideways {
			sideways++
		}
	}
	pct := float64(sideways) / float64(len(labeled)-warmup)
	require.Greater(t, pct, 0.4, "majority of choppy bars should be Sideways or Volatile, got %.1f%%", pct*100)
}

func TestVolatileRegimeDetected(t *testing.T) {
	bars := makeVolatileBars(200, 10000, 0.02) // high daily swings
	d := regime.New(regime.DefaultConfig())
	labeled := d.Classify(bars)

	warmup := regime.DefaultConfig().EMAPeriod
	volatile := 0
	for _, lb := range labeled[warmup:] {
		if lb.Regime == regime.Volatile {
			volatile++
		}
	}
	pct := float64(volatile) / float64(len(labeled)-warmup)
	require.Greater(t, pct, 0.4, "majority of high-volatility bars should be Volatile, got %.1f%%", pct*100)
}

func TestCoverageAlwaysSumsToOne(t *testing.T) {
	bars := makeChoppyBars(300, 50000, 0.003)
	d := regime.New(regime.DefaultConfig())
	labeled := d.Classify(bars)
	coverage := regime.Coverage(labeled)

	total := 0.0
	for _, v := range coverage {
		total += v
	}
	require.InDelta(t, 1.0, total, 0.001, "regime coverage must sum to 1.0")
}

func TestExtractWindowsContiguous(t *testing.T) {
	bars := append(
		makeTrendingBars(100, 10000, +0.003),
		makeTrendingBars(100, 10300, -0.003)...,
	)
	d := regime.New(regime.DefaultConfig())
	labeled := d.Classify(bars)
	windows := regime.ExtractWindows(labeled)

	require.NotEmpty(t, windows)
	// Every bar must be covered by exactly one window
	totalBars := 0
	for _, w := range windows {
		totalBars += w.Bars
	}
	require.Equal(t, len(bars), totalBars, "all bars must be covered by extracted windows")
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func makeTrendingBars(n int, start, drift float64) []regime.Bar {
	bars := make([]regime.Bar, n)
	price := start
	for i := range bars {
		price *= (1 + drift)
		bars[i] = regime.Bar{
			Time:  time.Now().Add(time.Duration(i) * 5 * time.Minute),
			Close: price,
		}
	}
	return bars
}

func makeChoppyBars(n int, center, amplitude float64) []regime.Bar {
	bars := make([]regime.Bar, n)
	for i := range bars {
		// Sinusoidal oscillation, no trend
		bars[i] = regime.Bar{
			Time:  time.Now().Add(time.Duration(i) * 5 * time.Minute),
			Close: center * (1 + amplitude*math.Sin(float64(i)*0.3)),
		}
	}
	return bars
}

func makeVolatileBars(n int, start, swingPct float64) []regime.Bar {
	bars := make([]regime.Bar, n)
	price := start
	// Alternating large up and down moves
	for i := range bars {
		if i%2 == 0 {
			price *= (1 + swingPct)
		} else {
			price *= (1 - swingPct*0.9)
		}
		bars[i] = regime.Bar{
			Time:  time.Now().Add(time.Duration(i) * 5 * time.Minute),
			Close: price,
		}
	}
	return bars
}
