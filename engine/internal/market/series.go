package market

import (
	"desk/internal/model"
	"math"
)

type IndicatorPoint struct {
	Time      int64    `json:"time"`
	CloseTime int64    `json:"closeTime,omitempty"`
	Closed    *bool    `json:"closed"`
	Close     float64  `json:"close"`
	EMA20     *float64 `json:"ema20"`
	MACD      *float64 `json:"macd"`
	Signal    *float64 `json:"signal"`
	Histogram *float64 `json:"histogram"`
	RSI7      *float64 `json:"rsi7"`
	ATR14     *float64 `json:"atr14"`
}

// All values use the same candle window and seed convention as the existing
// headline indicators. Unwarmed values are null, never fabricated zeroes.
func IndicatorSeries(candles []model.Candle) []IndicatorPoint {
	n := len(candles)
	out := make([]IndicatorPoint, n)
	closes := make([]float64, n)
	tr := make([]float64, n)
	for i, c := range candles {
		closes[i] = c.Close
		tr[i] = c.High - c.Low
		if i > 0 {
			tr[i] = math.Max(tr[i], math.Max(math.Abs(c.High-candles[i-1].Close), math.Abs(c.Low-candles[i-1].Close)))
		}
	}
	ema := EMA(closes, 20)
	fast := EMA(closes, 12)
	slow := EMA(closes, 26)
	dif := make([]float64, n)
	for i := 25; i < n; i++ {
		dif[i] = fast[i] - slow[i]
	}
	var signal []float64
	if n >= 26 {
		signal = EMA(dif[25:], 9)
	}
	var gain, loss, atr float64
	value := func(x float64) *float64 { return &x }
	for i, c := range candles {
		row := IndicatorPoint{Time: c.Time, CloseTime: c.CloseTime, Closed: c.Closed, Close: c.Close}
		if i >= 19 {
			row.EMA20 = value(ema[i])
		}
		if i >= 25 {
			row.MACD = value(dif[i])
		}
		if i >= 33 {
			row.Signal = value(signal[i-25])
			row.Histogram = value(dif[i] - signal[i-25])
		}
		if i > 0 {
			change := closes[i] - closes[i-1]
			g, l := math.Max(change, 0), math.Max(-change, 0)
			if i <= 7 {
				gain += g
				loss += l
				if i == 7 {
					gain /= 7
					loss /= 7
				}
			} else {
				gain = (gain*6 + g) / 7
				loss = (loss*6 + l) / 7
			}
			if i >= 7 {
				rsi := 100.0
				if loss > 0 {
					rsi = 100 - 100/(1+gain/loss)
				}
				row.RSI7 = value(rsi)
			}
		}
		if i < 14 {
			atr += tr[i]
			if i == 13 {
				atr /= 14
			}
		} else {
			atr = (atr*13 + tr[i]) / 14
		}
		if i >= 13 {
			row.ATR14 = value(atr)
		}
		out[i] = row
	}
	return out
}
