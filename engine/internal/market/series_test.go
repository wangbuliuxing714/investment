package market

import (
	"desk/internal/model"
	"math"
	"testing"
)

func TestIndicatorSeriesMatchesExistingAndWarmup(t *testing.T) {
	candles := make([]model.Candle, 100)
	closes := make([]float64, 100)
	closed := true
	for i := range candles {
		p := 100 + float64(i) + math.Sin(float64(i))*8
		closes[i] = p
		candles[i] = model.Candle{Time: int64(i + 1), CloseTime: int64(i+2) * 1000, Closed: &closed, Open: p, High: p + 2, Low: p - 2, Close: p}
	}
	rows := IndicatorSeries(candles)
	for i, r := range rows {
		if (r.EMA20 != nil) != (i >= 19) || (r.MACD != nil) != (i >= 25) || (r.Signal != nil) != (i >= 33) || (r.Histogram != nil) != (i >= 33) || (r.RSI7 != nil) != (i >= 7) || (r.ATR14 != nil) != (i >= 13) {
			t.Fatalf("warmup at %d: %+v", i, r)
		}
		if i >= 33 {
			m, s, h := MACD(closes[:i+1])
			for _, pair := range [][2]float64{{*r.MACD, m}, {*r.Signal, s}, {*r.Histogram, h}, {*r.EMA20, Last(EMA(closes[:i+1], 20))}, {*r.RSI7, RSI(closes[:i+1], 7)}} {
				if math.Abs(pair[0]-pair[1]) > 1e-10 {
					t.Fatal(i, pair)
				}
			}
		}
		if r.CloseTime != candles[i].CloseTime || r.Closed != &closed {
			t.Fatal("lost candle status")
		}
	}
}
func TestATRTrueRangeIncludesGaps(t *testing.T) {
	rows := make([]model.Candle, 15)
	for i := range rows {
		rows[i] = model.Candle{High: 102, Low: 98, Close: 100}
	}
	rows[14] = model.Candle{High: 111, Low: 108, Close: 110}
	series := IndicatorSeries(rows)
	if *series[13].ATR14 != 4 || math.Abs(*series[14].ATR14-4.5) > 1e-12 {
		t.Fatal(series[13], series[14])
	}
	if len(IndicatorSeries(nil)) != 0 {
		t.Fatal("empty")
	}
}
