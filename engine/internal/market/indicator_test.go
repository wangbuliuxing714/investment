package market

import (
	"math"
	"testing"
)

func TestMACDHasSignalAtThirtyFourSamples(t *testing.T) {
	closes := make([]float64, 34)
	for i := range closes {
		closes[i] = float64(i + 1)
	}
	m, s, h := MACD(closes)
	if math.Abs(m-7) > 1e-10 || math.Abs(s-7) > 1e-10 || math.Abs(h) > 1e-10 {
		t.Fatalf("incorrect EMA warmup boundary: %v %v %v", m, s, h)
	}
	m, s, h = MACD(closes[:33])
	if m != 0 || s != 0 || h != 0 {
		t.Fatal("insufficient warmup")
	}
}
