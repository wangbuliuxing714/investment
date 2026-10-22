package trader

import (
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// FloorToStep rounds a quantity or price down to the exchange step.
// The returned string is what Binance expects, without exponent notation.
func FloorToStep(value float64, step string) string {
	step = strings.TrimSpace(step)
	st, err := decimal.NewFromString(step)
	if err != nil || !st.IsPositive() {
		return trimFloat(value)
	}
	out := decimal.NewFromFloat(value).Div(st).Floor().Mul(st)
	if !out.IsPositive() {
		return "0"
	}
	return out.String()
}

func trimFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func stepDecimals(step string) int {
	step = strings.TrimRight(strings.TrimSpace(step), "0")
	if i := strings.IndexByte(step, '.'); i >= 0 {
		return len(step) - i - 1
	}
	return 0
}
