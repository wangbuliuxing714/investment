package trader

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"desk/internal/ai"
	"desk/internal/binance"
	"desk/internal/model"
)

// All exchange operations are in memory. These tests never send real orders.
type submitExchange struct {
	account                                      model.Account
	positions                                    []model.Position
	price                                        float64
	hedge                                        bool
	accountErr, positionsErr, hedgeErr, priceErr error
	emptyPrice, failAfterOrder                   bool
	orders                                       []binance.MarketOrder
	events                                       []string
	writes                                       int
}

func (f *submitExchange) Account(context.Context) (model.Account, error) {
	f.events = append(f.events, "account")
	if f.failAfterOrder && len(f.orders) > 0 {
		return model.Account{}, errors.New("refresh unavailable")
	}
	return f.account, f.accountErr
}
func (f *submitExchange) Positions(context.Context) ([]model.Position, error) {
	f.events = append(f.events, "positions")
	return append([]model.Position(nil), f.positions...), f.positionsErr
}
func (f *submitExchange) Hedge(context.Context) (bool, error) {
	f.events = append(f.events, "hedge")
	return f.hedge, f.hedgeErr
}
func (f *submitExchange) KlinesBefore(_ context.Context, symbol, interval string, limit int, end int64) ([]model.Candle, error) {
	f.events = append(f.events, fmt.Sprintf("price:%s:%s:%d:%d", symbol, interval, limit, end))
	if f.emptyPrice {
		return nil, f.priceErr
	}
	return []model.Candle{{Close: f.price}}, f.priceErr
}
func (f *submitExchange) Rules(context.Context, string) (string, string, float64, float64, error) {
	f.events = append(f.events, "rules")
	return "0.001", "0.01", 0.001, 5, nil
}
func (f *submitExchange) CancelProtection(context.Context, string, string) error {
	f.events = append(f.events, "cancel")
	f.writes++
	return nil
}
func (f *submitExchange) SetLeverage(context.Context, string, int) error {
	f.events = append(f.events, "leverage")
	f.writes++
	return nil
}
func (f *submitExchange) Stop(context.Context, binance.StopOrder) error {
	f.events = append(f.events, "stop")
	f.writes++
	return nil
}
func (f *submitExchange) Market(_ context.Context, order binance.MarketOrder) (binance.Fill, error) {
	f.events = append(f.events, "order")
	f.writes++
	f.orders = append(f.orders, order)
	if order.Reduce {
		f.positions = dropPosition(f.positions, order.Symbol, order.Side)
		f.account.MarginUsed = 0
	} else {
		f.positions = append(f.positions, model.Position{Symbol: order.Symbol, Side: order.Side, Amount: atof(order.Qty), Mark: f.price, Leverage: 5})
		f.account.MarginUsed += atof(order.Qty) * f.price / 5
	}
	return binance.Fill{OrderID: int64(len(f.orders)), Status: "FILLED", Qty: order.Qty, AvgPrice: fmt.Sprint(f.price)}, nil
}

func submitStrategy() model.Strategy {
	return model.Strategy{Coins: []string{"BTCUSDT"}, BTCETHLeverage: 5, AltLeverage: 3, MaxPositions: 2, PositionValueRatio: 1, MaxMarginUsage: 0.5}
}
func submitFixture() *submitExchange {
	return &submitExchange{account: model.Account{Equity: 1000, Wallet: 1000, Available: 1000}, price: 100}
}

func TestFreshTurnCanClosePreviouslyObservedPosition(t *testing.T) {
	for _, side := range []string{"long", "short"} {
		for _, hedge := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/hedge=%v", side, hedge), func(t *testing.T) {
				f := submitFixture()
				f.hedge = hedge
				f.positions = []model.Position{{Symbol: "BTCUSDT", Side: side, Amount: 1.058, Mark: 100, Leverage: 20}}
				previous := &agentState{client: f, strategy: submitStrategy()}
				if got := previous.call(context.Background(), ai.ToolCall{Name: "get_positions"}); !strings.Contains(got, "BTCUSDT "+side) {
					t.Fatal(got)
				}
				f.events = nil
				// Mirrors cloudLoop: the next model response gets a new empty state.
				current := &agentState{client: f, strategy: submitStrategy()}
				got := current.submit(context.Background(), fmt.Sprintf("{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"close_%s\"}]}", side))
				if !strings.Contains(got, "已平仓") || len(f.orders) != 1 {
					t.Fatalf("close failed: %s", got)
				}
				order := f.orders[0]
				if !order.Reduce || order.Hedge != hedge || order.Side != side || order.Qty != "1.058" {
					t.Fatalf("wrong close order: %+v", order)
				}
				if strings.Join(f.events, ",") != "account,positions,hedge,rules,order,cancel,account,positions" {
					t.Fatalf("wrong snapshot/order sequence: %v", f.events)
				}
			})
		}
	}
}

func TestOpenLoadsFreshEquityAndPriceAndRetainsRiskCaps(t *testing.T) {
	f := submitFixture()
	a := &agentState{client: f, strategy: submitStrategy(), marks: map[string]float64{"BTCUSDT": 1}}
	got := a.submit(context.Background(), "{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"open_long\",\"position_size_usd\":5000,\"leverage\":20}]}")
	if !strings.Contains(got, "已开仓") || len(f.orders) != 1 {
		t.Fatal(got)
	}
	if f.orders[0].Qty != "10" || f.orders[0].Reduce || a.marks["BTCUSDT"] != 100 {
		t.Fatalf("stale price or risk cap used: %+v", f.orders)
	}
	if !strings.Contains(got, "杠杆从 20 调整为 5") || !strings.Contains(got, "5000.00 调到 1000.00") {
		t.Fatal("risk caps not applied", got)
	}
	if !strings.Contains(strings.Join(f.events, ","), "price:BTCUSDT:1m:1:0") {
		t.Fatal("price not refreshed")
	}
}

func TestSnapshotAndPriceFailuresNeverUseStaleStateForOrders(t *testing.T) {
	for _, failure := range []string{"account", "positions", "hedge", "price", "emptyPrice", "zeroPrice", "negativePrice", "nanPrice", "infinitePrice"} {
		t.Run(failure, func(t *testing.T) {
			f := submitFixture()
			err := errors.New("test failure")
			switch failure {
			case "account":
				f.accountErr = err
			case "positions":
				f.positionsErr = err
			case "hedge":
				f.hedgeErr = err
			case "price":
				f.priceErr = err
			case "emptyPrice":
				f.emptyPrice = true
			case "zeroPrice":
				f.price = 0
			case "negativePrice":
				f.price = -1
			case "nanPrice":
				f.price = math.NaN()
			case "infinitePrice":
				f.price = math.Inf(1)
			}
			a := &agentState{client: f, strategy: submitStrategy(), account: model.Account{Equity: 1000}, marks: map[string]float64{"BTCUSDT": 100}}
			got := a.submit(context.Background(), "{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"open_long\",\"position_size_usd\":100,\"leverage\":5}]}")
			if f.writes != 0 || !strings.Contains(got, "未") {
				t.Fatalf("failed read allowed an exchange write: %s, events=%v", got, f.events)
			}
		})
	}
}

func TestBatchRefreshesReleasedMarginBeforeOpening(t *testing.T) {
	for _, failAfter := range []bool{false, true} {
		f := submitFixture()
		f.account.MarginUsed = 500
		f.positions = []model.Position{{Symbol: "BTCUSDT", Side: "long", Amount: 1, Mark: 100}}
		f.failAfterOrder = failAfter
		a := &agentState{client: f, strategy: submitStrategy()}
		got := a.submit(context.Background(), "{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"open_short\",\"position_size_usd\":100,\"leverage\":5},{\"symbol\":\"BTCUSDT\",\"action\":\"close_long\"}]}")
		if len(f.orders) == 0 || !f.orders[0].Reduce {
			t.Fatal("must close before opening", got)
		}
		if failAfter {
			if len(f.orders) != 1 || !strings.Contains(got, "该笔订单已执行") || !strings.Contains(got, "后续决定未执行") {
				t.Fatal("post-order refresh failure mishandled", got)
			}
		} else if len(f.orders) != 2 || f.orders[1].Reduce || f.orders[1].Side != "short" {
			t.Fatal("released margin not refreshed", got)
		}
	}
}

func TestEmptyPositionAndPaperModeDoNotSendOrders(t *testing.T) {
	for _, paper := range []bool{false, true} {
		f := submitFixture()
		if paper {
			f.positions = []model.Position{{Symbol: "BTCUSDT", Side: "long", Amount: 1, Mark: 100}}
		}
		a := &agentState{client: f, strategy: submitStrategy(), paper: paper}
		got := a.submit(context.Background(), "{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"close_long\"}]}")
		if f.writes != 0 {
			t.Fatal("unexpected exchange write", got)
		}
		if !paper && !strings.Contains(got, "未向币安发送平仓订单") {
			t.Fatal("missing local skip reason", got)
		}
		if paper && !strings.Contains(got, "没有向币安发单") {
			t.Fatal("paper protection missing", got)
		}
	}
}

func TestHoldAndDisallowedSymbolsNeedNoExchangeAccess(t *testing.T) {
	f := submitFixture()
	a := &agentState{client: f, strategy: submitStrategy()}
	got := a.submit(context.Background(), "{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"hold\"},{\"symbol\":\"SOLUSDT\",\"action\":\"open_long\",\"position_size_usd\":100,\"leverage\":5}]}")
	if len(f.events) != 0 || !strings.Contains(got, "不在策略币种里") || !strings.Contains(got, "观望") {
		t.Fatal(got, f.events)
	}
}
