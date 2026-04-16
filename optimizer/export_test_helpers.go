// Package optimizer – exported test helpers.
//
// checkThresholds is an internal function.  This file exports it as
// CheckThresholds so the test suite can verify individual threshold rules
// without running a full backtest.
package optimizer

// CheckThresholds is the exported version of checkThresholds for testing.
func CheckThresholds(r *EvalResult, t PromotionThresholds) (bool, []string) {
	return checkThresholds(r, t)
}
