// Package regime classifies market conditions into discrete regimes.
//
// A regime is a persistent market state that materially affects strategy
// performance.  The same strategy may be excellent in a trending bull market
// and destructive in a choppy sideways market.
//
// EvolvX uses regime labels to:
//   1. Tag historical bars so backtests can be split by regime
//   2. Score optimizer candidates per-regime (not just overall)
//   3. Block strategy promotion if it fails in any significant regime
//
// Detection algorithm: rule-based classification using three indicators —
// trend (price vs n-bar EMA), momentum (rolling return), and volatility
// (rolling standard deviation of returns).  No external ML library needed.
// This is deliberately simple and transparent — explainable to any trader.
package regime

import (
	"math"
	"sort"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Regime
// ─────────────────────────────────────────────────────────────────────────────

// Label is one of the four market regimes.
type Label string

const (
	Bull     Label = "bull"      // trending upward, low-medium vol
	Bear     Label = "bear"      // trending downward, rising vol
	Sideways Label = "sideways"  // range-bound, low directional momentum
	Volatile Label = "volatile"  // high volatility, no clear trend
)

// Bar is the minimum market data needed for regime classification.
type Bar struct {
	Time   time.Time
	Close  float64
	Volume float64
}

// LabeledBar attaches a regime label to a bar.
type LabeledBar struct {
	Bar
	Regime     Label
	TrendScore float64 // positive = bullish, negative = bearish
	VolScore   float64 // 0–1, higher = more volatile
}

// ─────────────────────────────────────────────────────────────────────────────
// Detector
// ─────────────────────────────────────────────────────────────────────────────

// DetectorConfig controls the classification thresholds.
type DetectorConfig struct {
	// EMAPeriod: lookback for trend EMA (default 50)
	EMAPeriod int
	// MomentumPeriod: lookback for rolling return (default 20)
	MomentumPeriod int
	// VolPeriod: lookback for volatility measurement (default 20)
	VolPeriod int
	// BullThreshold: min positive momentum to classify as bull (default 0.03 = 3%)
	BullThreshold float64
	// BearThreshold: max negative momentum to classify as bear (default -0.03)
	BearThreshold float64
	// VolThreshold: normalised vol above which = volatile regime (default 0.6)
	VolThreshold float64
}

// DefaultConfig returns sensible defaults for crypto 5m bars.
func DefaultConfig() DetectorConfig {
	return DetectorConfig{
		EMAPeriod:      50,
		MomentumPeriod: 20,
		VolPeriod:      20,
		BullThreshold:  0.03,
		BearThreshold:  -0.03,
		VolThreshold:   0.6,
	}
}

// Detector classifies a series of bars into regime labels.
type Detector struct {
	cfg DetectorConfig
}

// New creates a Detector with cfg.
func New(cfg DetectorConfig) *Detector {
	return &Detector{cfg: cfg}
}

// Classify labels every bar in the input slice.
// Bars must be sorted chronologically oldest-first.
// Returns the same number of LabeledBars as input bars.
// Bars before the warmup window (EMAPeriod) receive the Sideways label.
func (d *Detector) Classify(bars []Bar) []LabeledBar {
	n := len(bars)
	out := make([]LabeledBar, n)

	if n == 0 {
		return out
	}

	closes := make([]float64, n)
	for i, b := range bars {
		closes[i] = b.Close
	}

	// Compute EMA
	emas := computeEMA(closes, d.cfg.EMAPeriod)

	// Compute rolling returns
	returns := make([]float64, n)
	for i := 1; i < n; i++ {
		if closes[i-1] > 0 {
			returns[i] = (closes[i] - closes[i-1]) / closes[i-1]
		}
	}

	// Compute rolling momentum (sum of returns over period)
	momentums := rollingSum(returns, d.cfg.MomentumPeriod)

	// Compute rolling volatility (std dev of returns)
	vols := rollingStdDev(returns, d.cfg.VolPeriod)

	// Normalise volatility to 0–1 range for thresholding
	maxVol := 0.0
	for _, v := range vols {
		if v > maxVol {
			maxVol = v
		}
	}

	for i, b := range bars {
		lb := LabeledBar{Bar: b, Regime: Sideways}

		if i < d.cfg.EMAPeriod {
			out[i] = lb
			continue
		}

		momentum := momentums[i]
		volNorm := 0.0
		if maxVol > 0 {
			volNorm = vols[i] / maxVol
		}
		aboveEMA := closes[i] > emas[i]

		lb.TrendScore = momentum
		lb.VolScore = volNorm

		switch {
		case volNorm > d.cfg.VolThreshold:
			lb.Regime = Volatile
		case momentum >= d.cfg.BullThreshold && aboveEMA:
			lb.Regime = Bull
		case momentum <= d.cfg.BearThreshold && !aboveEMA:
			lb.Regime = Bear
		default:
			lb.Regime = Sideways
		}

		out[i] = lb
	}

	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// RegimeSplit — partitions a bar series by regime for backtesting
// ─────────────────────────────────────────────────────────────────────────────

// Split groups labeled bars by regime.  The minimum segment length is
// enforced so that tiny flickers (< minBars) are merged into the prior regime.
func Split(labeled []LabeledBar, minBars int) map[Label][]LabeledBar {
	if minBars <= 0 {
		minBars = 10
	}
	out := map[Label][]LabeledBar{
		Bull: {}, Bear: {}, Sideways: {}, Volatile: {},
	}

	// First pass: group consecutive same-regime runs
	type run struct {
		label Label
		bars  []LabeledBar
	}
	var runs []run
	for _, lb := range labeled {
		if len(runs) == 0 || runs[len(runs)-1].label != lb.Regime {
			runs = append(runs, run{label: lb.Regime, bars: []LabeledBar{lb}})
		} else {
			runs[len(runs)-1].bars = append(runs[len(runs)-1].bars, lb)
		}
	}

	// Second pass: merge short runs into previous
	for i := range runs {
		if len(runs[i].bars) < minBars && i > 0 {
			runs[i].label = runs[i-1].label
		}
	}

	// Collect into map
	for _, r := range runs {
		out[r.label] = append(out[r.label], r.bars...)
	}

	return out
}

// Coverage returns what fraction of bars fall into each regime.
func Coverage(labeled []LabeledBar) map[Label]float64 {
	counts := map[Label]int{Bull: 0, Bear: 0, Sideways: 0, Volatile: 0}
	for _, lb := range labeled {
		counts[lb.Regime]++
	}
	total := float64(len(labeled))
	result := make(map[Label]float64, 4)
	for k, v := range counts {
		if total > 0 {
			result[k] = float64(v) / total
		}
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// RegimeSummary — per-regime performance metrics
// ─────────────────────────────────────────────────────────────────────────────

// RegimeMetrics holds the performance of one strategy evaluation within a
// specific regime.
type RegimeMetrics struct {
	Regime      Label   `json:"regime"`
	BarCount    int     `json:"bar_count"`
	Coverage    float64 `json:"coverage_pct"` // fraction of total test period
	NetReturn   float64 `json:"net_return"`
	MaxDrawdown float64 `json:"max_drawdown"`
	SharpeRatio float64 `json:"sharpe_ratio"`
	WinRate     float64 `json:"win_rate"`
	Trades      int     `json:"trades"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Math helpers
// ─────────────────────────────────────────────────────────────────────────────

func computeEMA(prices []float64, period int) []float64 {
	emas := make([]float64, len(prices))
	if len(prices) == 0 || period <= 0 {
		return emas
	}
	k := 2.0 / float64(period+1)
	// Seed with SMA
	sum := 0.0
	start := min2(period, len(prices))
	for i := 0; i < start; i++ {
		sum += prices[i]
	}
	emas[start-1] = sum / float64(start)
	for i := start; i < len(prices); i++ {
		emas[i] = prices[i]*k + emas[i-1]*(1-k)
	}
	return emas
}

func rollingSum(v []float64, period int) []float64 {
	out := make([]float64, len(v))
	for i := period; i < len(v); i++ {
		s := 0.0
		for j := i - period; j < i; j++ {
			s += v[j]
		}
		out[i] = s
	}
	return out
}

func rollingStdDev(v []float64, period int) []float64 {
	out := make([]float64, len(v))
	for i := period; i < len(v); i++ {
		window := v[i-period : i]
		mu := 0.0
		for _, x := range window {
			mu += x
		}
		mu /= float64(period)
		variance := 0.0
		for _, x := range window {
			d := x - mu
			variance += d * d
		}
		out[i] = math.Sqrt(variance / float64(period))
	}
	return out
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─────────────────────────────────────────────────────────────────────────────
// TimeRange — maps a regime label to a list of [from, to] time windows
// ─────────────────────────────────────────────────────────────────────────────

// TimeWindow is a contiguous time period in which a single regime was active.
type TimeWindow struct {
	Label Label
	From  time.Time
	To    time.Time
	Bars  int
}

// ExtractWindows converts labeled bars into contiguous time windows per regime.
// These windows are passed to the multi-symbol backtest runner.
func ExtractWindows(labeled []LabeledBar) []TimeWindow {
	if len(labeled) == 0 {
		return nil
	}
	var windows []TimeWindow
	cur := TimeWindow{
		Label: labeled[0].Regime,
		From:  labeled[0].Time,
		To:    labeled[0].Time,
		Bars:  1,
	}
	for _, lb := range labeled[1:] {
		if lb.Regime == cur.Label {
			cur.To = lb.Time
			cur.Bars++
		} else {
			windows = append(windows, cur)
			cur = TimeWindow{Label: lb.Regime, From: lb.Time, To: lb.Time, Bars: 1}
		}
	}
	windows = append(windows, cur)

	// Sort by from time
	sort.Slice(windows, func(i, j int) bool {
		return windows[i].From.Before(windows[j].From)
	})
	return windows
}
