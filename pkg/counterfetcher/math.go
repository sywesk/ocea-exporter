package counterfetcher

import "math"

// round3 rounds a float64 to 3 decimals.
func round3(f float64) float64 {
	return math.Round(f*1000) / 1000
}
