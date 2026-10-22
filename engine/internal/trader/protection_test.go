package trader

import (
	"context"
	"desk/internal/binance"
	"desk/internal/model"
	"errors"
	"strings"
	"sync"
	"testing"
)

type protectiveExchange struct {
	*submitExchange
	levels     []model.Level
	stops      []binance.StopOrder
	readError  bool
	failKind   string
	hidePlaced bool
}

func (f *protectiveExchange) ChartOrders(context.Context, string) ([]model.Level, error) {
	if f.readError {
		return nil, errors.New("unavailable")
	}
	return f.levels, nil
}
func (f *protectiveExchange) Stop(_ context.Context, o binance.StopOrder) error {
	f.stops = append(f.stops, o)
	if o.Kind == f.failKind {
		return errors.New("rejected")
	}
	kind := "stop"
	if o.Kind == "take_profit" {
		kind = "take"
	}
	if !f.hidePlaced {
		f.levels = append(f.levels, model.Level{Kind: kind, Side: o.Side, Price: atof(o.Price), Protective: true, WorkingType: "MARK_PRICE"})
	}
	return nil
}
func protectionFixture() *protectiveExchange {
	f := &protectiveExchange{submitExchange: submitFixture()}
	f.positions = []model.Position{{Symbol: "BTCUSDT", Side: "long", Amount: 1, Mark: 100}}
	return f
}
func TestProtectionPartialFailureRetryOnlyAddsMissing(t *testing.T) {
	f := protectionFixture()
	f.failKind = "take_profit"
	req := ProtectionRequest{Symbol: "BTCUSDT", Side: "long", StopLoss: 90, TakeProfit: 110}
	first, err := ProtectPosition(context.Background(), f, req)
	if err != nil || first.Confirmed || first.StopLoss != 90 || first.TakeProfit != 0 || len(f.stops) != 2 {
		t.Fatal(first, f.stops, err)
	}
	f.failKind = ""
	second, err := ProtectPosition(context.Background(), f, req)
	if err != nil || !second.Confirmed || len(f.stops) != 3 || f.stops[2].Kind != "take_profit" || len(f.orders) != 0 {
		t.Fatal(second, f.stops, err)
	}
}

func TestConcurrentProtectionRepairsDoNotDuplicateOrders(t *testing.T) {
	f := protectionFixture()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := ProtectPosition(context.Background(), f, ProtectionRequest{Symbol: "BTCUSDT", Side: "long", StopLoss: 90, TakeProfit: 110})
			if err != nil || !result.Confirmed {
				t.Errorf("%+v %v", result, err)
			}
		}()
	}
	wg.Wait()
	if len(f.stops) != 2 {
		t.Fatal("duplicate conditionals", f.stops)
	}
}
func TestProtectionRequiresPositionValidDirectionAndKnownOrders(t *testing.T) {
	for _, failure := range []string{"no-position", "wrong-side", "wrong-stop", "wrong-take", "read"} {
		f := protectionFixture()
		req := ProtectionRequest{Symbol: "BTCUSDT", Side: "long", StopLoss: 90, TakeProfit: 110}
		switch failure {
		case "no-position":
			f.positions = nil
		case "wrong-side":
			req.Side = "short"
		case "wrong-stop":
			req.StopLoss = 101
		case "wrong-take":
			req.TakeProfit = 99
		case "read":
			f.readError = true
		}
		if _, err := ProtectPosition(context.Background(), f, req); err == nil || len(f.stops) > 0 {
			t.Fatal(failure, err, f.stops)
		}
	}
}
func TestProtectionDoesNotTreatAcceptedButAbsentOrderAsConfirmed(t *testing.T) {
	f := protectionFixture()
	f.hidePlaced = true
	result, err := ProtectPosition(context.Background(), f, ProtectionRequest{Symbol: "BTCUSDT", Side: "long", StopLoss: 90})
	if err != nil || result.Confirmed || !strings.Contains(result.Message, "没有本地") {
		t.Fatal(result, err)
	}
}
func TestAgentProtectionRespectsPaperAndAllowedSymbols(t *testing.T) {
	for _, paper := range []bool{true, false} {
		f := protectionFixture()
		a := agentState{client: f, strategy: submitStrategy(), paper: paper}
		symbol := "BTCUSDT"
		if !paper {
			symbol = "ETHUSDT"
		}
		a.protect(context.Background(), `{"symbol":"`+symbol+`","side":"long","stop_loss":90}`)
		if len(f.stops) > 0 || len(f.orders) > 0 {
			t.Fatal("unauthorized exchange write")
		}
	}
	if !knownTool("set_protection") || readOnlyTool("set_protection") {
		t.Fatal("protection must be a sequential write tool")
	}
}

type failedClose struct{ *submitExchange }

func (f failedClose) Market(context.Context, binance.MarketOrder) (binance.Fill, error) {
	return binance.Fill{}, errors.New("market rejected")
}
func TestFailedCloseKeepsProtection(t *testing.T) {
	f := submitFixture()
	positions := []model.Position{{Symbol: "BTCUSDT", Side: "long", Amount: 1, Mark: 100}}
	_, after, changed := executePlan(context.Background(), failedClose{f}, false, positions, 100, Plan{Decision: model.Decision{Symbol: "BTCUSDT", Action: "close_long"}}, false)
	if changed || len(after) != 1 || strings.Contains(strings.Join(f.events, ","), "cancel") {
		t.Fatal("failed close removed protection", f.events)
	}
}
