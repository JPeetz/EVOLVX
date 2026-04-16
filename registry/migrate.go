// Package registry – migration from the existing store.strategy table.
//
// The existing nofx codebase stores strategy config in a SQLite table called
// "strategies" (or similar).  This file provides a one-time importer that
// reads those rows and creates versioned StrategyRecord entries in the new
// registry.
package registry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// LegacyStrategyRow is a minimal representation of what the existing
// store.StrategyConfig looks like in the database.  Adjust field names if
// the actual schema differs.
type LegacyStrategyRow struct {
	ID        int64
	Name      string
	Config    string // JSON of store.StrategyConfig
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MigrateFromLegacy reads all rows from the legacy SQLite database at
// legacyDBPath and imports them into the registry service svc.
//
// It is idempotent: strategies that already have a record in the registry
// (matched by name) are skipped.
func MigrateFromLegacy(legacyDBPath string, svc *Service) error {
	legacyDB, err := sql.Open("sqlite3", legacyDBPath)
	if err != nil {
		return fmt.Errorf("migrate: open legacy db: %w", err)
	}
	defer legacyDB.Close()

	// Probe the schema — the table name in production is "auto_traders"
	// which stores an embedded strategy_config JSON column.
	rows, err := legacyDB.Query(`
		SELECT id, name, strategy_config, created_at
		FROM   auto_traders
		ORDER  BY id ASC
	`)
	if err != nil {
		// Fall back to "strategies" table if auto_traders doesn't exist
		rows, err = legacyDB.Query(`
			SELECT id, name, config, created_at
			FROM   strategies
			ORDER  BY id ASC
		`)
		if err != nil {
			return fmt.Errorf("migrate: query legacy strategies: %w", err)
		}
	}
	defer rows.Close()

	imported := 0
	skipped := 0

	for rows.Next() {
		var row LegacyStrategyRow
		var createdAtStr string
		if err := rows.Scan(&row.ID, &row.Name, &row.Config, &createdAtStr); err != nil {
			log.Printf("migrate: scan row: %v", err)
			continue
		}
		row.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)

		// Parse legacy config into Parameters
		params, err := parseLegacyConfig(row.Config)
		if err != nil {
			log.Printf("migrate: parse config for %q: %v", row.Name, err)
			continue
		}

		// Check if already imported
		_, err = svc.db.QueryRow(`SELECT 1 FROM strategies WHERE name=?`, row.Name).Scan(new(int))
		if err == nil {
			skipped++
			continue
		}

		r := &StrategyRecord{
			ID:          uuid.NewString(),
			Name:        row.Name,
			Version:     "1.0.0",
			Author:      "migrated",
			CreatedAt:   row.CreatedAt,
			Status:      StatusDraft,
			StatusChangedAt: time.Now(),
			Parameters:  params,
			RawConfig:   row.Config, // preserve original JSON
			CompatibleMarkets:    []string{"crypto"},
			CompatibleTimeframes: params.SelectedTimeframes,
		}

		if _, err := svc.Create(r); err != nil {
			log.Printf("migrate: create %q: %v", row.Name, err)
			continue
		}
		imported++
	}

	log.Printf("migrate: imported %d strategies, skipped %d", imported, skipped)
	return rows.Err()
}

// parseLegacyConfig converts the existing strategy_config JSON into the new
// typed Parameters struct.  Unknown fields are silently ignored.
func parseLegacyConfig(raw string) (Parameters, error) {
	// The existing StrategyConfig has a slightly different shape.
	// We unmarshal into a generic map and then map fields.
	var cfg struct {
		CoinSource struct {
			SourceType    string   `json:"source_type"`
			StaticCoins   []string `json:"static_coins"`
			UseCoinPool   bool     `json:"use_coin_pool"`
			UseOITop      bool     `json:"use_oi_top"`
			CoinPoolLimit int      `json:"coin_pool_limit"`
		} `json:"coin_source"`
		Indicators struct {
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
			Klines            struct {
				PrimaryTimeframe   string   `json:"primary_timeframe"`
				SelectedTimeframes []string `json:"selected_timeframes"`
				PrimaryCount       int      `json:"primary_count"`
			} `json:"klines"`
		} `json:"indicators"`
		RiskControl struct {
			MaxPositions                   int     `json:"max_positions"`
			BTCETHMaxLeverage              int     `json:"btceth_max_leverage"`
			AltcoinMaxLeverage             int     `json:"altcoin_max_leverage"`
			BTCETHMaxPositionValueRatio    float64 `json:"btceth_max_position_value_ratio"`
			AltcoinMaxPositionValueRatio   float64 `json:"altcoin_max_position_value_ratio"`
			MaxMarginUsage                 float64 `json:"max_margin_usage"`
			MinPositionSize                float64 `json:"min_position_size"`
			MinRiskRewardRatio             float64 `json:"min_risk_reward_ratio"`
			MinConfidence                  int     `json:"min_confidence"`
		} `json:"risk_control"`
		PromptSections struct {
			RoleDefinition   string `json:"role_definition"`
			TradingFrequency string `json:"trading_frequency"`
			EntryStandards   string `json:"entry_standards"`
			DecisionProcess  string `json:"decision_process"`
		} `json:"prompt_sections"`
		TradingMode  string `json:"trading_mode"`
		CustomPrompt string `json:"custom_prompt"`
	}

	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return Parameters{}, fmt.Errorf("parseLegacyConfig: %w", err)
	}

	p := Parameters{
		CoinSourceType:  cfg.CoinSource.SourceType,
		StaticCoins:     cfg.CoinSource.StaticCoins,
		UseCoinPool:     cfg.CoinSource.UseCoinPool,
		UseOITop:        cfg.CoinSource.UseOITop,
		CoinPoolLimit:   cfg.CoinSource.CoinPoolLimit,

		EnableEMA:         cfg.Indicators.EnableEMA,
		EMAPeriods:        cfg.Indicators.EMAPeriods,
		EnableMACD:        cfg.Indicators.EnableMACD,
		EnableRSI:         cfg.Indicators.EnableRSI,
		RSIPeriods:        cfg.Indicators.RSIPeriods,
		EnableATR:         cfg.Indicators.EnableATR,
		ATRPeriods:        cfg.Indicators.ATRPeriods,
		EnableVolume:      cfg.Indicators.EnableVolume,
		EnableOI:          cfg.Indicators.EnableOI,
		EnableFundingRate: cfg.Indicators.EnableFundingRate,
		EnableQuantData:   cfg.Indicators.EnableQuantData,

		PrimaryTimeframe:   cfg.Indicators.Klines.PrimaryTimeframe,
		SelectedTimeframes: cfg.Indicators.Klines.SelectedTimeframes,
		PrimaryCount:       cfg.Indicators.Klines.PrimaryCount,

		MaxPositions:                   cfg.RiskControl.MaxPositions,
		BTCETHMaxLeverage:              cfg.RiskControl.BTCETHMaxLeverage,
		AltcoinMaxLeverage:             cfg.RiskControl.AltcoinMaxLeverage,
		BTCETHMaxPositionValueRatio:    cfg.RiskControl.BTCETHMaxPositionValueRatio,
		AltcoinMaxPositionValueRatio:   cfg.RiskControl.AltcoinMaxPositionValueRatio,
		MaxMarginUsage:                 cfg.RiskControl.MaxMarginUsage,
		MinPositionSize:                cfg.RiskControl.MinPositionSize,
		MinRiskRewardRatio:             cfg.RiskControl.MinRiskRewardRatio,
		MinConfidence:                  cfg.RiskControl.MinConfidence,

		TradingMode:      cfg.TradingMode,
		RoleDefinition:   cfg.PromptSections.RoleDefinition,
		TradingFrequency: cfg.PromptSections.TradingFrequency,
		EntryStandards:   cfg.PromptSections.EntryStandards,
		DecisionProcess:  cfg.PromptSections.DecisionProcess,
		CustomPrompt:     cfg.CustomPrompt,
	}

	// Set sensible defaults for zero-value fields
	if p.MaxPositions == 0 {
		p.MaxPositions = 3
	}
	if p.MinPositionSize == 0 {
		p.MinPositionSize = 12
	}
	if p.PrimaryTimeframe == "" {
		p.PrimaryTimeframe = "5m"
	}
	if len(p.SelectedTimeframes) == 0 {
		p.SelectedTimeframes = []string{"5m", "15m", "1h", "4h"}
	}

	return p, nil
}
