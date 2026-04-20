// Package compaction – test exports.
package compaction

import "github.com/NoFxAiOS/nofx/registry"

// RetentionDays is exported for testing.
func (p *RetentionPolicy) RetentionDays(strategyID string, status registry.StrategyStatus) int {
	return p.retentionDays(strategyID, status)
}
