package trader

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"desk/internal/model"
)

type historyFixture struct {
	rows      []model.Candle
	seenLimit int
	seenEnd   int64
}

func (f *historyFixture) KlinesBefore(_ context.Context, _ string, _ string, limit int, end int64) ([]model.Candle, error) {
	f.seenLimit, f.seenEnd = limit, end
	var rows []model.Candle
	for _, row := range f.rows {
		if end == 0 || (end < 1_000_000_000_000 && row.Time < end) || (end >= 1_000_000_000_000 && row.Time*1000 <= end) {
			rows = append(rows, row)
		}
	}
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	return rows, nil
}

func candlesForTest(count int, step int64) []model.Candle {
	rows := make([]model.Candle, count)
	for i := range rows {
		rows[i] = model.Candle{Time: 1790640000 + int64(i)*step, Open: 100, High: 102, Low: 99, Close: float64(i + 1), Volume: 0.0000123}
	}
	return rows
}

func TestMarketWindowIsChosenByAgentNotToday(t *testing.T) {
	f := &historyFixture{rows: candlesForTest(1600, 15*60)}
	a := &agentState{marketSource: f, strategy: model.Strategy{Coins: []string{"BTCUSDT", "ETHUSDT"}}}
	for _, n := range []int{20, 200, 1500} {
		got := a.market(context.Background(), fmt.Sprintf("{\"symbol\":\"BTCUSDT\",\"interval\":\"15m\",\"limit\":%d}", n))
		if f.seenLimit != n || !strings.Contains(got, fmt.Sprintf("requested=%d count=%d", n, n)) {
			t.Fatal("chosen window overridden", n)
		}
		for _, timeValue := range []int64{f.rows[len(f.rows)-n].Time, f.rows[len(f.rows)-1].Time} {
			if !strings.Contains(got, candleTime(timeValue, time.UTC)) || !strings.Contains(got, candleTime(timeValue, beijingTime)) {
				t.Fatal("coverage or timezone missing")
			}
		}
		if n == 20 && !strings.Contains(got, "MACD(12,26,9) N/A") {
			t.Fatal("missing indicator appeared as zero")
		}
	}
	got := a.market(context.Background(), "{}")
	if f.seenLimit != 200 || !strings.Contains(got, "symbol=BTCUSDT") || !strings.Contains(got, "symbol=ETHUSDT") {
		t.Fatal("default window or multiple symbols lost")
	}
}

func TestSecondCandlePaginationHasNoSkippedBoundary(t *testing.T) {
	f := &historyFixture{rows: candlesForTest(5, 1)}
	a := &agentState{marketSource: f}
	first := a.market(context.Background(), "{\"symbol\":\"BTCUSDT\",\"interval\":\"1s\",\"limit\":2}")
	wantCursor := f.rows[3].Time
	if !strings.Contains(first, fmt.Sprintf("nextEnd=%d", wantCursor)) {
		t.Fatal("cursor skips one second", first)
	}
	lastMark := a.marks["BTCUSDT"]
	older := a.market(context.Background(), fmt.Sprintf("{\"symbol\":\"BTCUSDT\",\"interval\":\"1s\",\"limit\":2,\"end\":%d}", wantCursor))
	if !strings.Contains(older, candleTime(f.rows[2].Time, time.UTC)) || strings.Contains(older, candleTime(f.rows[3].Time, time.UTC)) {
		t.Fatal("page boundary skipped or repeated")
	}
	if a.marks["BTCUSDT"] != lastMark || !strings.Contains(older, "不是当前实时报价") {
		t.Fatal("historical page treated as live")
	}
}

func TestMarketFormattingKeepsPrecisionAndIndicatorAvailability(t *testing.T) {
	rows := candlesForTest(1, 1)
	rows[0].Open, rows[0].High, rows[0].Low, rows[0].Close = 0.00001234, 0.00001236, 0.00001231, 0.00001235
	got := formatCoin(coinPack{Symbol: "TESTUSDT", Interval: "1s", Candles: rows})
	for _, want := range []string{"0.00001234", "0.00001235", "0.0000123", "EMA20 N/A", "MACD(12,26,9) N/A", "RSI7 N/A", "2026-09-29 00:00:00", "本页最后一根"} {
		if !strings.Contains(got, want) {
			t.Fatal("missing precise value or availability marker", want, got)
		}
	}
	got = formatCoin(coinPack{Symbol: "BTCUSDT", Interval: "1m", Candles: candlesForTest(34, 60)})
	if strings.Contains(got, "N/A（至少") {
		t.Fatal("enough samples incorrectly unavailable")
	}
}
