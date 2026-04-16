package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NoFxAiOS/nofx/registry"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test: Strategy versions are immutable
// ─────────────────────────────────────────────────────────────────────────────

func TestStrategyVersionImmutability(t *testing.T) {
	svc := newTestRegistry(t)

	original := baseRecord()
	created, err := svc.Create(original)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", created.Version)

	// Mutate params and create a new version
	mutated := created.Parameters
	mutated.MinConfidence = 99
	child, err := svc.NewVersion(
		created.ID, created.Version,
		"patch", "tester",
		mutated, "raised confidence",
	)
	require.NoError(t, err)
	require.Equal(t, "1.0.1", child.Version, "patch bump must produce 1.0.1")
	require.Equal(t, 99, child.Parameters.MinConfidence)

	// Original version must be unchanged
	orig, err := svc.GetVersion(created.ID, "1.0.0")
	require.NoError(t, err)
	require.Equal(t, original.Parameters.MinConfidence, orig.Parameters.MinConfidence,
		"original version must not be mutated by NewVersion")

	// Listing shows both versions
	versions, err := svc.ListVersions(created.ID)
	require.NoError(t, err)
	require.Len(t, versions, 2, "both versions must exist")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Status transitions follow the lifecycle
// ─────────────────────────────────────────────────────────────────────────────

func TestStatusTransitions(t *testing.T) {
	svc := newTestRegistry(t)

	r, _ := svc.Create(baseRecord())

	// draft → paper is allowed
	require.NoError(t, svc.SetStatus(r.ID, r.Version, registry.StatusPaper, "tester"))

	// Get the newly created patch version that carries the new status
	latest, err := svc.GetLatest(r.ID)
	require.NoError(t, err)
	require.Equal(t, registry.StatusPaper, latest.Status)

	// paper → approved requires non-empty changedBy
	err = svc.SetStatus(latest.ID, latest.Version, registry.StatusApproved, "")
	require.ErrorIs(t, err, registry.ErrApprovalRequired,
		"human approval gate must block empty changedBy")

	// paper → approved with a real approver
	require.NoError(t, svc.SetStatus(latest.ID, latest.Version, registry.StatusApproved, "j.peetz69@gmail.com"))

	approved, _ := svc.GetLatest(r.ID)
	require.Equal(t, registry.StatusApproved, approved.Status)

	// approved → draft is NOT in the valid transitions
	err = svc.SetStatus(approved.ID, approved.Version, registry.StatusDraft, "someone")
	require.ErrorIs(t, err, registry.ErrInvalidStatus)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Export/Import round-trip is lossless
// ─────────────────────────────────────────────────────────────────────────────

func TestExportImportRoundTrip(t *testing.T) {
	svc := newTestRegistry(t)

	r, _ := svc.Create(baseRecord())
	data, err := registry.Export(r)
	require.NoError(t, err)

	imported, err := registry.Import(data)
	require.NoError(t, err)
	require.Equal(t, r.ID, imported.ID)
	require.Equal(t, r.Version, imported.Version)
	require.Equal(t, r.Parameters.MinConfidence, imported.Parameters.MinConfidence)
	require.Equal(t, r.Parameters.MaxPositions, imported.Parameters.MaxPositions)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Multiple minor and major bumps are tracked in lineage
// ─────────────────────────────────────────────────────────────────────────────

func TestVersionLineageTracking(t *testing.T) {
	svc := newTestRegistry(t)
	r, _ := svc.Create(baseRecord())

	// 1.0.0 → 1.1.0 (minor)
	v110, err := svc.NewVersion(r.ID, "1.0.0", "minor", "tester", r.Parameters, "added RSI14")
	require.NoError(t, err)
	require.Equal(t, "1.1.0", v110.Version)

	// 1.1.0 → 2.0.0 (major)
	v200, err := svc.NewVersion(r.ID, "1.1.0", "major", "tester", r.Parameters, "full strategy overhaul")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", v200.Version)

	lineage, err := svc.GetLineage(r.ID)
	require.NoError(t, err)
	require.Len(t, lineage, 3, "initial + two bumps = 3 lineage nodes")

	require.Equal(t, "initial creation", lineage[0].MutationSummary)
	require.Equal(t, "added RSI14", lineage[1].MutationSummary)
	require.Equal(t, "full strategy overhaul", lineage[2].MutationSummary)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: GetLatest always returns the highest semver
// ─────────────────────────────────────────────────────────────────────────────

func TestGetLatestReturnHighestSemver(t *testing.T) {
	svc := newTestRegistry(t)
	r, _ := svc.Create(baseRecord())

	svc.NewVersion(r.ID, "1.0.0", "patch", "a", r.Parameters, "p1")  // 1.0.1
	svc.NewVersion(r.ID, "1.0.0", "minor", "a", r.Parameters, "m1") // 1.1.0
	svc.NewVersion(r.ID, "1.0.0", "major", "a", r.Parameters, "ma") // 2.0.0

	latest, err := svc.GetLatest(r.ID)
	require.NoError(t, err)
	require.Equal(t, "2.0.0", latest.Version,
		"GetLatest must return highest semver, not most recently inserted row")
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func newTestRegistry(t *testing.T) *registry.Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.db")
	svc, err := registry.New(path)
	require.NoError(t, err)
	t.Cleanup(func() {
		svc.Close()
		os.Remove(path)
	})
	return svc
}

func baseRecord() *registry.StrategyRecord {
	return &registry.StrategyRecord{
		Name:   "test-strategy",
		Author: "tester",
		Parameters: registry.Parameters{
			CoinSourceType:      "static",
			StaticCoins:         []string{"BTCUSDT"},
			PrimaryTimeframe:    "5m",
			SelectedTimeframes:  []string{"5m", "15m"},
			PrimaryCount:        30,
			MaxPositions:        3,
			MinPositionSize:     12,
			MinConfidence:       75,
			TradingMode:         "conservative",
			AltcoinMaxLeverage:  5,
			BTCETHMaxLeverage:   5,
			MaxMarginUsage:      0.8,
		},
		CompatibleMarkets:    []string{"crypto"},
		CompatibleTimeframes: []string{"5m"},
	}
}
