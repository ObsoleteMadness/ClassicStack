package telemetry

import (
	"math"
	"strconv"
)

func i64string(v int64) string {
	return strconv.FormatInt(v, 10)
}

func f64string(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func float64frombits(b uint64) float64 { return math.Float64frombits(b) }
func float64tobits(f float64) uint64   { return math.Float64bits(f) }
