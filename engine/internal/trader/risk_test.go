package trader

import (
	"testing"

	"desk/internal/model"
)

func testStrategy() model.Strategy {
	return model.Strategy{
		Coins:              []string{"BTCUSDT", "ETHUSDT"},
		BTCETHLeverage:     5,
		AltLeverage:        3,
		MaxPositions:       2,
		PositionValueRatio: 1,
		MaxMarginUsage:     0.5,
	}
}

func TestApplyCapsNotionalAndLeverage(t *testing.T) {
	plan := Apply(testStrategy(), 1000, 100, nil, 100, model.Decision{
		Symbol: "BTCUSDT", Action: "open_long", Leverage: 20, PositionSizeUSD: 5000,
		StopLoss: 90, TakeProfit: 80,
	})
	if plan.Skip != "" {
		t.Fatal(plan.Skip)
	}
	if plan.Leverage != 5 {
		t.Fatalf("leverage %d", plan.Leverage)
	}
	if plan.Notional > 1000.01 {
		t.Fatalf("notional %v", plan.Notional)
	}
	if plan.Decision.TakeProfit != 0 {
		t.Fatal("bad take profit should be dropped")
	}
	if plan.Decision.StopLoss != 90 {
		t.Fatal("stop should stay")
	}
}

func TestApplyMaxPositions(t *testing.T) {
	positions := []model.Position{{Symbol: "BTCUSDT", Side: "long", Amount: 1}, {Symbol: "ETHUSDT", Side: "short", Amount: 1}}
	plan := Apply(testStrategy(), 1000, 0, positions, 10, model.Decision{Symbol: "BTCUSDT", Action: "open_long", Leverage: 2, PositionSizeUSD: 100})
	if plan.Skip == "" {
		t.Fatal("expected skip")
	}
}

func TestApplyMarginRoom(t *testing.T) {
	plan := Apply(testStrategy(), 1000, 500, nil, 100, model.Decision{Symbol: "ETHUSDT", Action: "open_short", Leverage: 3, PositionSizeUSD: 400, StopLoss: 110, TakeProfit: 90})
	if plan.Skip != "保证金已到上限" {
		t.Fatalf("skip %q", plan.Skip)
	}
}
