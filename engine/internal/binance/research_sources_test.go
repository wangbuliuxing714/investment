package binance

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestDemoStatisticsDefaultToExplicitlyLabelledPublicReference(t *testing.T) {
	for _, symbol := range []string{"BTCUSDT", "ETHUSDT", "SUIUSDT"} {
		for _, name := range []string{"get_open_interest", "get_order_flow"} {
			t.Run(symbol+"/"+name, func(t *testing.T) {
				requests := 0
				c := analyticClient(t, func(r *http.Request) (int, string) {
					requests++
					q := r.URL.Query()
					if r.URL.Host != "fapi.binance.com" || r.Header.Get("X-MBX-APIKEY") != "" || q.Get("signature") != "" || q.Get("symbol") != symbol {
						t.Fatal("reference source/credential boundary violated", r.URL)
					}
					if r.URL.Path == "/fapi/v1/openInterest" {
						return 200, fmt.Sprintf(`{"symbol":%q,"openInterest":"100","time":1790772300000}`, symbol)
					}
					if q.Get("period") != "1h" || q.Get("limit") != "24" || q.Get("startTime") != "1790685900000" || q.Get("endTime") != "1790772300000" {
						t.Fatal("lost caller history parameters", q)
					}
					if name == "get_open_interest" {
						return 200, `[{"sumOpenInterest":"100","sumOpenInterestValue":"10000","timestamp":1790772300000}]`
					}
					return 200, `[{"buyVol":"3","sellVol":"1","buySellRatio":"3","timestamp":1790772300000}]`
				})
				out := c.Analytics(context.Background(), name, AnalyticsRequest{Symbol: symbol, Period: "1h", Limit: 24, Start: 1790685900, End: 1790772300})
				if out.Status != "ok" || !out.Source.ReferenceOnly || out.Source.Environment != "live_reference" || out.Source.TradingEnvironment != "demo" || out.Request.Environment != "live_reference" {
					t.Fatalf("missing accurate provenance: %+v", out)
				}
				if !c.Testnet() || c.raw.BaseURL != demoBase || c.raw.APIKey != "demo-key" || c.raw.SecretKey != "demo-secret" {
					t.Fatal("research changed execution client")
				}
				expected := 1
				if name == "get_open_interest" {
					expected = 2
				}
				if requests != expected {
					t.Fatal("unexpected retries or mixed sources", requests)
				}
			})
		}
	}
}
func TestOtherDemoResearchDefaultsStayDemo(t *testing.T) {
	for _, name := range []string{"get_indicators", "get_funding", "get_order_book", "get_trading_history"} {
		t.Run(name, func(t *testing.T) {
			c := analyticClient(t, func(r *http.Request) (int, string) {
				if r.URL.Host != "demo-fapi.binance.com" {
					t.Fatal("unrelated tool source changed", r.URL.Host)
				}
				return 503, `{"code":-1}`
			})
			out := c.Analytics(context.Background(), name, AnalyticsRequest{Symbol: "BTCUSDT"})
			if out.Source.Environment != "demo" || out.Source.TradingEnvironment != "demo" || out.Source.ReferenceOnly {
				t.Fatal(out.Source)
			}
		})
	}
}
func TestLiveStatisticsDefaultToActiveProduction(t *testing.T) {
	c := newClient("live-key", "live-secret", false, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "fapi.binance.com" || r.Header.Get("X-MBX-APIKEY") != "" {
			t.Fatal("bad live read")
		}
		return nil, fmt.Errorf("unavailable")
	})})
	out := c.Analytics(context.Background(), "get_order_flow", AnalyticsRequest{Symbol: "BTCUSDT"})
	if out.Source.Environment != "live" || out.Source.TradingEnvironment != "live" || out.Source.ReferenceOnly || out.Status != "unavailable" {
		t.Fatal(out)
	}
}
