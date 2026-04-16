// api/router_additions.go
//
// This file shows the exact lines to add to the existing api/router.go
// (or server.go) to mount the new registry, journal, and optimizer endpoints.
//
// In the existing code, the Gin router is typically set up in api/server.go
// or api/router.go like:
//
//   r := gin.New()
//   api.RegisterRoutes(r)   ← existing
//
// Add the following block AFTER the existing route registration:
//
//   v1 := r.Group("/api/v1")
//
//   registryGroup := v1.Group("/registry")
//   api.RegisterRegistryRoutes(registryGroup, registryService)
//
//   journalGroup := v1.Group("/journal")
//   api.RegisterJournalRoutes(journalGroup, journalService)
//
//   optimizerGroup := v1.Group("/optimizer")
//   api.RegisterOptimizerRoutes(optimizerGroup, optimizerService)
//
// The services are constructed once at startup in main.go:

package api

import (
	"log"
	"os"
	"path/filepath"

	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/optimizer"
	"github.com/NoFxAiOS/nofx/registry"
)

// Services holds all the new service singletons.
// Construct this once in main.go and pass it to the server.
type Services struct {
	Registry  *registry.Service
	Journal   *journal.Service
	Optimizer *optimizer.Service
}

// NewServices constructs all services from a single base data directory.
//
//   dataDir: the directory where nofx stores its databases
//            (same directory as the existing nofx.db)
//
// backtest runner is provided by the caller because it needs access to the
// existing decision engine which lives in a separate package.
func NewServices(dataDir string, backtestRunner optimizer.BacktestRunner) (*Services, error) {
	regPath := filepath.Join(dataDir, "registry.db")
	journalPath := filepath.Join(dataDir, "journal.db")
	optPath := filepath.Join(dataDir, "optimizer.db")

	reg, err := registry.New(regPath)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}

	j, err := journal.New(journalPath)
	if err != nil {
		reg.Close()
		return nil, fmt.Errorf("journal: %w", err)
	}

	opt, err := optimizer.New(optPath, reg, backtestRunner, 4)
	if err != nil {
		reg.Close()
		j.Close()
		return nil, fmt.Errorf("optimizer: %w", err)
	}

	return &Services{Registry: reg, Journal: j, Optimizer: opt}, nil
}

// Close releases all services.
func (s *Services) Close() {
	if s.Registry != nil {
		if err := s.Registry.Close(); err != nil {
			log.Printf("registry close: %v", err)
		}
	}
	if s.Journal != nil {
		if err := s.Journal.Close(); err != nil {
			log.Printf("journal close: %v", err)
		}
	}
	if s.Optimizer != nil {
		if err := s.Optimizer.Close(); err != nil {
			log.Printf("optimizer close: %v", err)
		}
	}
}

// RunMigrationIfNeeded checks whether the legacy database has been migrated
// and runs the migration if not.  Safe to call on every startup.
func RunMigrationIfNeeded(legacyDBPath string, svc *Services) {
	// Check if the registry already has entries
	strategies, err := svc.Registry.ListByStatus(registry.StatusDraft)
	if err == nil && len(strategies) > 0 {
		log.Printf("registry already has %d strategies — skipping migration", len(strategies))
		return
	}

	if _, err := os.Stat(legacyDBPath); os.IsNotExist(err) {
		log.Printf("no legacy database at %s — skipping migration", legacyDBPath)
		return
	}

	log.Printf("running one-time strategy migration from %s", legacyDBPath)
	if err := registry.MigrateFromLegacy(legacyDBPath, svc.Registry); err != nil {
		log.Printf("migration error (non-fatal): %v", err)
	}
}

var _ = fmt.Errorf
