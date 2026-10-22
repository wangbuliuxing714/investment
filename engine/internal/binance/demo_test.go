package binance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMarketOrderSideForOpeningAndClosingInBothPositionModes(t *testing.T) {
	for _, hedge := range []bool{false, true} {
		for _, reduce := range []bool{false, true} {
			for _, side := range []string{"long", "short"} {
				t.Run(fmt.Sprintf("%s/close=%v/hedge=%v", side, reduce, hedge), func(t *testing.T) {
					requests := 0
					c := newClient("demo-key", "demo-secret", true, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) {
						requests++
						if r.URL.Host != "demo-fapi.binance.com" || r.URL.Path != "/fapi/v1/order" || r.Method != http.MethodPost {
							t.Fatal("unexpected order route", r.Method, r.URL.Host, r.URL.Path)
						}
						if err := r.ParseForm(); err != nil {
							t.Fatal(err)
						}
						wantSide := "BUY"
						if (side == "long" && reduce) || (side == "short" && !reduce) {
							wantSide = "SELL"
						}
						if r.Form.Get("side") != wantSide {
							t.Fatalf("wrong side: got %s want %s", r.Form.Get("side"), wantSide)
						}
						if hedge {
							if r.Form.Get("positionSide") != strings.ToUpper(side) || r.Form.Get("reduceOnly") != "" {
								t.Fatal("wrong hedge mode flags")
							}
						} else {
							if r.Form.Get("positionSide") != "" {
								t.Fatal("unexpected position side in one-way mode")
							}
							if (r.Form.Get("reduceOnly") == "true") != reduce {
								t.Fatal("wrong reduceOnly flag")
							}
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{\"orderId\":321,\"status\":\"FILLED\",\"avgPrice\":\"100\",\"executedQty\":\"1\"}"))}, nil
					})})
					if _, err := c.Market(context.Background(), MarketOrder{Symbol: "BTCUSDT", Side: side, Qty: "1", Hedge: hedge, Reduce: reduce}); err != nil {
						t.Fatal(err)
					}
					if requests != 1 {
						t.Fatalf("expected one order request, got %d", requests)
					}
				})
			}
		}
	}
}

type demoTransport func(*http.Request) (*http.Response, error)

func (f demoTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDemoRequestsAndOrdersUseOnlyDemoHost(t *testing.T) {
	requests := 0
	transport := demoTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Scheme != "https" || r.URL.Host != "demo-fapi.binance.com" {
			t.Fatalf("demo request escaped its environment: %s", r.URL.Host)
		}
		if key := r.Header.Get("X-MBX-APIKEY"); key != "" && key != "demo-key" {
			t.Fatal("unexpected credential on demo request")
		}
		body := `{}`
		switch {
		case strings.HasSuffix(r.URL.Path, "/time"):
			body = `{"serverTime":1790696082677}`
		case strings.HasSuffix(r.URL.Path, "/account"):
			body = `{"totalMarginBalance":"10000","totalWalletBalance":"10000","availableBalance":"9000","totalUnrealizedProfit":"0"}`
		case strings.HasSuffix(r.URL.Path, "/positionRisk"):
			body = `[]`
		case strings.HasSuffix(r.URL.Path, "/positionSide/dual"):
			body = `{"dualSidePosition":false}`
		case strings.HasSuffix(r.URL.Path, "/klines"):
			body = `[[1790696040000,"10","12","9","11","20",1790696099999,"200",5,"10","100","0"]]`
		case strings.HasSuffix(r.URL.Path, "/order"):
			body = `{"orderId":123,"status":"FILLED","avgPrice":"11","executedQty":"1"}`
		case strings.HasSuffix(r.URL.Path, "/algoOrder"):
			body = `{"algoId":456,"algoStatus":"NEW"}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	c := newClient("demo-key", "demo-secret", true, &http.Client{Transport: transport})
	ctx := context.Background()
	c.syncTime()
	account, err := c.Account(ctx)
	if err != nil || !account.Testnet || account.Equity != 10000 {
		t.Fatalf("demo account: %v", err)
	}
	if _, err := c.Positions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Hedge(ctx); err != nil {
		t.Fatal(err)
	}
	if candles, err := c.Klines(ctx, "BTCUSDT", "1m", 2); err != nil || len(candles) != 1 {
		t.Fatalf("demo candles: %v", err)
	}
	if fill, err := c.Market(ctx, MarketOrder{Symbol: "BTCUSDT", Side: "long", Qty: "1"}); err != nil || fill.OrderID != 123 {
		t.Fatalf("demo order: %v", err)
	}
	if err := c.Stop(ctx, StopOrder{Symbol: "BTCUSDT", Side: "long", Price: "9"}); err != nil {
		t.Fatal(err)
	}
	if err := c.CancelAll(ctx, "BTCUSDT"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetLeverage(ctx, "BTCUSDT", 2); err != nil {
		t.Fatal(err)
	}
	if requests != 9 {
		t.Fatalf("expected all nine requests to use demo, got %d", requests)
	}
}

func TestDemoFailureDoesNotRetryOnProduction(t *testing.T) {
	requests := 0
	c := newClient("demo-key", "demo-secret", true, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "demo-fapi.binance.com" {
			t.Fatal("a failed demo request fell back to production")
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":-2015,"msg":"Invalid API-key"}`))}, nil
	})})
	if _, err := c.Market(context.Background(), MarketOrder{Symbol: "BTCUSDT", Side: "long", Qty: "1"}); err == nil {
		t.Fatal("expected the demo credential error")
	}
	if requests != 1 {
		t.Fatalf("unexpected retry count: %d", requests)
	}
}
