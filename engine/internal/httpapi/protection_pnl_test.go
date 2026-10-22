package httpapi

import (
	"strings"
	"testing"
	"time"
)

func TestPnLRangeUsesBeijingMidnight(t *testing.T) {
	now := time.Date(2026, 9, 30, 2, 3, 4, 0, time.UTC)
	from, err := pnlRange("day", now)
	if err != nil || from.UTC() != time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC) {
		t.Fatal(from, err)
	}
	for _, days := range []struct {
		key string
		n   int
	}{{"7d", 7}, {"30d", 30}} {
		from, err = pnlRange(days.key, now)
		if err != nil || now.Sub(from) != time.Duration(days.n)*24*time.Hour {
			t.Fatal(from, err)
		}
	}
	if _, err := pnlRange("all", now); err == nil {
		t.Fatal("invalid period accepted")
	}
}
func TestStreamEnvironmentAndTimestamp(t *testing.T) {
	if !strings.HasPrefix(klineStreamURL(true, "BTCUSDT", "15m"), "wss://fstream.binancefuture.com/market/ws/btcusdt@kline_") {
		t.Fatal("demo stream mixed with production")
	}
	if !strings.HasPrefix(klineStreamURL(false, "BTCUSDT", "15m"), "wss://fstream.binance.com/") {
		t.Fatal("wrong production stream")
	}
	raw := []byte(`{"stream":"x","data":{"e":"kline","E":1790691752365,"k":{"t":1790691720000,"T":1790691779999,"o":"100","h":"102","l":"99","c":"101","v":"1"}}}`)
	c, ok := parseLiveKline(raw)
	if !ok || c.UpdatedAt != 1790691752365 || c.Source != "websocket" || c.Time != 1790691720 {
		t.Fatal(c, ok)
	}
}
