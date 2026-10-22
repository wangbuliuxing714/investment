package market

// EMA returns the exponential moving average. Index 0 of a short series is 0
// until the window is filled, then each value uses the standard multiplier.
func EMA(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if period <= 0 || len(values) < period {
		return out
	}
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	out[period-1] = sum / float64(period)
	k := 2.0 / float64(period+1)
	for i := period; i < len(values); i++ {
		out[i] = values[i]*k + out[i-1]*(1-k)
	}
	return out
}

func Last(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

// MACD returns the last MACD line, signal line and histogram (12, 26, 9).
func MACD(closes []float64) (macd, signal, hist float64) {
	if len(closes) < 34 {
		return 0, 0, 0
	}
	fast := EMA(closes, 12)
	slow := EMA(closes, 26)
	line := make([]float64, len(closes))
	for i := range closes {
		if i >= 25 {
			line[i] = fast[i] - slow[i]
		}
	}
	sig := EMA(line[25:], 9)
	if len(sig) == 0 {
		return 0, 0, 0
	}
	macd = line[len(line)-1]
	signal = sig[len(sig)-1]
	hist = macd - signal
	return macd, signal, hist
}

// RSI is Wilder's relative strength index over the last `period` changes.
func RSI(closes []float64, period int) float64 {
	if period <= 0 || len(closes) <= period {
		return 0
	}
	gain, loss := 0.0, 0.0
	for i := 1; i <= period; i++ {
		d := closes[i] - closes[i-1]
		if d >= 0 {
			gain += d
		} else {
			loss -= d
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)
	for i := period + 1; i < len(closes); i++ {
		d := closes[i] - closes[i-1]
		g, l := 0.0, 0.0
		if d >= 0 {
			g = d
		} else {
			l = -d
		}
		avgGain = (avgGain*float64(period-1) + g) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + l) / float64(period)
	}
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}
