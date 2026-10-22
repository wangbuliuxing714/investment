package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func fixtureResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestProtectionAlgoRouteAndSignature(t *testing.T) {
	for _, demo := range []bool{false, true} {
		for _, hedge := range []bool{false, true} {
			for _, side := range []string{"long", "short"} {
				for _, kind := range []string{"stop", "take_profit"} {
					c := newClient("test-key", "test-secret", demo, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) {
						host := "fapi.binance.com"
						if demo {
							host = "demo-fapi.binance.com"
						}
						if r.URL.Host != host || r.URL.Path != "/fapi/v1/algoOrder" || r.Method != "POST" {
							t.Fatalf("wrong route %s", r.URL)
						}
						q := r.URL.Query()
						sig := q.Get("signature")
						q.Del("signature")
						mac := hmac.New(sha256.New, []byte("test-secret"))
						mac.Write([]byte(q.Encode()))
						if sig != hex.EncodeToString(mac.Sum(nil)) || r.Header.Get("X-MBX-APIKEY") != "test-key" {
							t.Fatal("invalid signature")
						}
						wantType := "STOP_MARKET"
						if kind == "take_profit" {
							wantType = "TAKE_PROFIT_MARKET"
						}
						wantSide := "SELL"
						if side == "short" {
							wantSide = "BUY"
						}
						if q.Get("type") != wantType || q.Get("side") != wantSide || q.Get("algoType") != "CONDITIONAL" || q.Get("triggerPrice") != "100.01" || q.Get("closePosition") != "true" || q.Get("workingType") != "MARK_PRICE" {
							t.Fatal("invalid conditional parameters", q)
						}
						for _, field := range []string{"quantity", "reduceOnly", "stopPrice"} {
							if q.Has(field) {
								t.Fatal("incompatible closePosition field", field)
							}
						}
						wantPosition := ""
						if hedge {
							wantPosition = strings.ToUpper(side)
						}
						if q.Get("positionSide") != wantPosition {
							t.Fatal("wrong hedge side")
						}
						return fixtureResponse(`{"algoId":123,"algoStatus":"NEW"}`), nil
					})})
					if err := c.Stop(context.Background(), StopOrder{Symbol: "BTCUSDT", Side: side, Kind: kind, Price: "100.01", Hedge: hedge}); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}

func TestProtectionRejectsUnconfirmedReceipt(t *testing.T) {
	for _, body := range []string{`{}`, `{"algoId":123,"algoStatus":"REJECTED"}`, `{"code":-4120,"msg":"wrong route"}`} {
		c := newClient("k", "s", true, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) { return fixtureResponse(body), nil })})
		if c.Stop(context.Background(), StopOrder{Symbol: "BTCUSDT", Side: "long", Price: "100"}) == nil {
			t.Fatal("false success", body)
		}
	}
}

func TestCancelProtectionPreservesOppositeHedgeAndOpeningOrders(t *testing.T) {
	var deleted []string
	c := newClient("k", "s", true, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "demo-fapi.binance.com" {
			t.Fatal("escaped demo")
		}
		if r.Method == "DELETE" {
			deleted = append(deleted, r.URL.Path+":"+r.URL.Query().Get("orderId")+r.URL.Query().Get("algoId"))
			return fixtureResponse(`{}`), nil
		}
		switch r.URL.Path {
		case "/fapi/v1/openOrders":
			return fixtureResponse(`[{"orderId":1,"type":"STOP_MARKET","side":"SELL","positionSide":"LONG"},{"orderId":2,"type":"STOP_MARKET","side":"BUY","positionSide":"SHORT"},{"orderId":3,"type":"LIMIT","side":"SELL","positionSide":"LONG"}]`), nil
		case "/fapi/v1/openAlgoOrders":
			return fixtureResponse(`[{"algoId":4,"orderType":"STOP_MARKET","side":"SELL","positionSide":"LONG"},{"algoId":5,"orderType":"STOP_MARKET","side":"BUY","positionSide":"SHORT"},{"algoId":6,"orderType":"STOP_MARKET","side":"BUY","positionSide":"LONG"}]`), nil
		}
		t.Fatal("unexpected route", r.URL.Path)
		return nil, nil
	})})
	if err := c.CancelProtection(context.Background(), "BTCUSDT", "long"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(deleted, ",") != "/fapi/v1/order:1,/fapi/v1/algoOrder:4" {
		t.Fatal(deleted)
	}
}

func TestPositionsProtectionKnownAndSideMatching(t *testing.T) {
	for _, fail := range []bool{false, true} {
		c := newClient("k", "s", true, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/fapi/v2/positionRisk":
				return fixtureResponse(`[{"symbol":"BTCUSDT","positionSide":"LONG","positionAmt":"1","markPrice":"100"},{"symbol":"BTCUSDT","positionSide":"SHORT","positionAmt":"-1","markPrice":"100"}]`), nil
			case "/fapi/v1/openOrders":
				return fixtureResponse("[]"), nil
			case "/fapi/v1/openAlgoOrders":
				if fail {
					return nil, fmt.Errorf("orders unavailable")
				}
				return fixtureResponse(`[{"symbol":"BTCUSDT","orderType":"STOP_MARKET","side":"SELL","positionSide":"LONG","triggerPrice":"90","closePosition":true,"workingType":"MARK_PRICE"}]`), nil
			}
			t.Fatal(r.URL.Path)
			return nil, nil
		})})
		rows, err := c.PositionsWithProtection(context.Background())
		if err != nil || len(rows) != 2 {
			t.Fatal(rows, err)
		}
		for _, p := range rows {
			if p.ProtectionKnown == fail {
				t.Fatal(p)
			}
			if fail && p.ProtectionError == "" {
				t.Fatal("missing read error")
			}
			if !fail && ((p.Side == "long" && len(p.Protection) != 1) || (p.Side == "short" && len(p.Protection) != 0)) {
				t.Fatal(p)
			}
		}
	}
}

func TestIncomePaginationDedupAndSeparateFees(t *testing.T) {
	calls := 0
	c := newClient("k", "s", true, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if r.URL.Path != "/fapi/v1/income" || q.Get("startTime") != "100" || q.Get("endTime") != "200" || q.Get("limit") != "1000" || q.Get("page") != fmt.Sprint(calls) {
			t.Fatal(r.URL)
		}
		var rows []incomeRow
		if calls == 1 {
			for i := 1; i <= 1000; i++ {
				rows = append(rows, incomeRow{Asset: "USDT", IncomeType: "REALIZED_PNL", Income: "1", TranID: json.Number(fmt.Sprint(i)), Time: 150})
			}
		} else {
			rows = []incomeRow{{"USDT", "REALIZED_PNL", "1", "1000", 150}, {"USDT", "REALIZED_PNL", "5", "1001", 150}, {"USDT", "COMMISSION", "-2", "1002", 150}, {"USDT", "FUNDING_FEE", "-1", "1003", 150}, {"USDT", "TRANSFER", "9999", "1004", 150}, {"BNB", "REALIZED_PNL", "99", "1005", 150}, {"USDT", "REALIZED_PNL", "99", "1006", 99}}
		}
		body, _ := json.Marshal(rows)
		return fixtureResponse(string(body)), nil
	})})
	pnl, err := c.IncomeSummary(context.Background(), 100, 200)
	if err != nil || calls != 2 || pnl.Realized != 1005 || pnl.Commission != -2 || pnl.Funding != -1 || pnl.Net != 1002 {
		t.Fatal(pnl, calls, err)
	}
}

func TestIncomeFailureNeverBecomesZeroSuccess(t *testing.T) {
	for _, body := range []string{`{"msg":"failure"}`, `[{"incomeType":"REALIZED_PNL","asset":"USDT","income":"1","time":150}]`, `[{"incomeType":"REALIZED_PNL","asset":"USDT","income":"NaN","time":150,"tranId":1}]`} {
		c := newClient("k", "s", true, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) { return fixtureResponse(body), nil })})
		if _, err := c.IncomeSummary(context.Background(), 100, 200); err == nil {
			t.Fatal("invalid summary accepted", body)
		}
	}
}
