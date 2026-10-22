package binance

import (
	"strconv"
	"testing"
)

func TestEndMillis(t *testing.T) {
	if endMillis(0) != 0 || endMillis(-5) != 0 {
		t.Fatal(endMillis(0), endMillis(-5))
	}
	if endMillis(1_700_000_000) != 1_700_000_000_000-1 {
		t.Fatal(endMillis(1_700_000_000))
	}
	if endMillis(1_700_000_000_000) != 1_700_000_000_000 {
		t.Fatal(endMillis(1_700_000_000_000))
	}
}

func TestChartOrders(t *testing.T) {
	rows := chartOrders([]openOrder{
		{Type: "STOP_MARKET", Side: "SELL", PositionSide: "LONG", Stop: "100", Close: true},
		{Type: "TAKE_PROFIT_MARKET", Side: "SELL", PositionSide: "LONG", Trigger: "150", Close: true},
		{Type: "STOP_MARKET", Side: "BUY", PositionSide: "SHORT", Stop: "80"},
		{Type: "LIMIT", Side: "BUY", Price: "90"},
		{Type: "MARKET", Side: "BUY", Price: "1"},
		{Type: "TRAILING_STOP_MARKET", Side: "SELL", PositionSide: "LONG", Activate: "110"},
		{Type: "STOP", Side: "SELL", PositionSide: "BOTH", Stop: "0", Price: "0"},
		{Type: "STOP_MARKET", Side: "SELL", PositionSide: "BOTH", Stop: "70", Reduce: true},
		{Type: "STOP_MARKET", Side: "SELL", PositionSide: "LONG", Stop: "100", Close: true},
	})
	want := []string{
		"order long 90",
		"stop long 70",
		"stop long 100",
		"stop short 80",
		"take long 150",
		"trail long 110",
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d lines, want %d", len(rows), len(want))
	}
	for i, line := range rows {
		got := line.Kind + " " + line.Side + " " + strconv.FormatFloat(line.Price, 'f', -1, 64)
		if got != want[i] {
			t.Fatalf("line %d: got %s, want %s", i, got, want[i])
		}
	}
}
