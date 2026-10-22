package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func analyticClient(t *testing.T, handle func(*http.Request) (int, string)) *Client {
	t.Helper()
	return newClient("demo-key", "demo-secret", true, &http.Client{Transport: demoTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatal("analytics attempted write", r.Method)
		}
		code, body := handle(r)
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
}
func TestAnalyticsEnvironmentIsolationAndUnavailable(t *testing.T) {
	for _, env := range []string{"", "active", "live_reference"} {
		t.Run(env, func(t *testing.T) {
			c := analyticClient(t, func(r *http.Request) (int, string) {
				host := "demo-fapi.binance.com"
				if env == "live_reference" || env == "" {
					host = "fapi.binance.com"
				}
				if r.URL.Host != host || r.Header.Get("X-MBX-APIKEY") != "" || r.URL.Query().Get("signature") != "" {
					t.Fatal("environment or credential leak")
				}
				return 200, "ok"
			})
			out := c.Analytics(context.Background(), "get_order_flow", AnalyticsRequest{Symbol: "BTCUSDT", Environment: env})
			if out.Status != "unavailable" || len(out.Data) != 0 || out.Source.ReferenceOnly != (env == "live_reference" || env == "") {
				t.Fatalf("%+v", out)
			}
		})
	}
	c := analyticClient(t, func(r *http.Request) (int, string) { t.Fatal("invalid request made network call"); return 200, "[]" })
	for _, r := range []AnalyticsRequest{{Symbol: "BTCUSDT", Environment: "live_reference"}, {Symbol: "BTCUSDT", Environment: "demo"}, {Symbol: "BTCUSDT", Start: 2000000000000, End: 1900000000000}} {
		if c.Analytics(context.Background(), "get_trading_history", r).Status != "unavailable" {
			t.Fatal(r)
		}
	}
}
func TestAnalyticsFundingAndOIPartial(t *testing.T) {
	c := analyticClient(t, func(r *http.Request) (int, string) {
		switch r.URL.Path {
		case "/fapi/v1/openInterest":
			return 200, `{"symbol":"BTCUSDT","openInterest":"100","time":1790760000000}`
		case "/fapi/v1/premiumIndex":
			return 200, `{"markPrice":"100","indexPrice":"101","lastFundingRate":"0.0001","nextFundingTime":1790784000000,"time":1790760000000}`
		default:
			return 503, `{"code":-1}`
		}
	})
	for _, name := range []string{"get_open_interest", "get_funding"} {
		out := c.Analytics(context.Background(), name, AnalyticsRequest{Symbol: "BTCUSDT"})
		if out.Status != "partial" || out.Data["current"] == nil || out.Data["history"] != nil || out.Errors["history"] == "" || out.FetchedAt == "" {
			t.Fatalf("%+v", out)
		}
	}
	for _, body := range []string{`{"openInterest":"NaN","time":1}`, `{"time":1}`, "null", `{"code":-1121,"msg":"bad symbol"}`} {
		c := analyticClient(t, func(r *http.Request) (int, string) { return 200, body })
		if out := c.Analytics(context.Background(), "get_open_interest", AnalyticsRequest{Symbol: "BTCUSDT"}); out.Status != "unavailable" {
			t.Fatalf("bad data succeeded: %+v", out)
		}
	}
}
func TestAnalyticsOrderFlowWindowAndOIPercent(t *testing.T) {
	c := analyticClient(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("period") != "5m" && r.URL.Path != "/fapi/v1/openInterest" {
			t.Fatal("period missing")
		}
		switch r.URL.Path {
		case "/futures/data/takerlongshortRatio":
			return 200, `[{"buyVol":"2","sellVol":"4","buySellRatio":"0.5","timestamp":1790760600000},{"buyVol":"3","sellVol":"1","buySellRatio":"3","timestamp":1790760300000}]`
		case "/fapi/v1/openInterest":
			return 200, `{"openInterest":"120","time":1790760600000}`
		default:
			return 200, `[{"sumOpenInterest":"100","sumOpenInterestValue":"10000","timestamp":1790760300000},{"sumOpenInterest":"120","sumOpenInterestValue":"12000","timestamp":1790760600000}]`
		}
	})
	out := c.Analytics(context.Background(), "get_order_flow", AnalyticsRequest{Symbol: "BTCUSDT"})
	rows := out.Data["flow"].(map[string]any)["rows"].([]map[string]any)
	if out.Status != "ok" || rows[0]["delta"] != 2.0 || rows[1]["windowCVD"] != 0.0 || rows[0]["imbalance"] != 0.5 {
		t.Fatal(rows)
	}
	out = c.Analytics(context.Background(), "get_open_interest", AnalyticsRequest{Symbol: "BTCUSDT"})
	rows = out.Data["history"].(map[string]any)["rows"].([]map[string]any)
	if rows[0]["change"] != nil || rows[1]["change"] != 20.0 || rows[1]["changePercent"] != 20.0 {
		t.Fatal(rows)
	}
}
func TestAnalyticsBookTwoSidesAndInsufficientDepth(t *testing.T) {
	c := analyticClient(t, func(r *http.Request) (int, string) {
		return 200, `{"lastUpdateId":1,"E":1790760600001,"T":1790760600000,"bids":[["98","1"],["99","1"]],"asks":[["102","1"],["101","1"]]}`
	})
	for _, amount := range []float64{150, 300} {
		out := c.Analytics(context.Background(), "get_order_book", AnalyticsRequest{Symbol: "BTCUSDT", NotionalUSD: amount})
		book := out.Data["book"].(map[string]any)
		if book["mid"] != 100.0 || book["spread"] != 2.0 || book["spreadBps"] != 200.0 {
			t.Fatal(book)
		}
		buy := book["buyEstimate"].(SlippageEstimate)
		sell := book["sellEstimate"].(SlippageEstimate)
		if amount == 150 {
			if !buy.FullyCovered || !sell.FullyCovered || math.Abs(*buy.FullOrderVWAP-101.33333333333333) > 1e-9 || math.Abs(*sell.FullOrderVWAP-98.66666666666667) > 1e-9 {
				t.Fatal(buy, sell)
			}
		} else {
			if buy.FullyCovered || buy.FullOrderVWAP != nil || buy.SlippageBps != nil || buy.FilledQuantity != 2 || sell.FullyCovered {
				t.Fatal(buy, sell)
			}
		}
	}
	for _, body := range []string{`{"lastUpdateId":1,"E":1,"T":1,"bids":[["103","1"]],"asks":[["101","1"]]}`, `{"lastUpdateId":1,"E":1,"T":1,"bids":[],"asks":[]}`, `{"lastUpdateId":1,"E":1,"T":1,"bids":[["99","NaN"]],"asks":[["101","1"]]}`} {
		bad := analyticClient(t, func(r *http.Request) (int, string) { return 200, body })
		if bad.Analytics(context.Background(), "get_order_book", AnalyticsRequest{Symbol: "BTCUSDT"}).Status != "unavailable" {
			t.Fatal("invalid book succeeded")
		}
	}
}
func TestAnalyticsIncomeCompleteVsFillSample(t *testing.T) {
	now := time.Now().UnixMilli()
	from := now - int64(30*24*time.Hour/time.Millisecond)
	c := analyticClient(t, func(r *http.Request) (int, string) {
		q := r.URL.Query()
		if r.URL.Host != "demo-fapi.binance.com" || q.Get("signature") == "" || q.Get("symbol") != "BTCUSDT" {
			t.Fatal("private source changed")
		}
		switch r.URL.Path {
		case "/fapi/v1/income":
			return 200, fmt.Sprintf(`[{"asset":"USDT","incomeType":"REALIZED_PNL","income":"20","tranId":1,"time":%d},{"asset":"USDT","incomeType":"COMMISSION","income":"-2","tranId":2,"time":%d},{"asset":"USDT","incomeType":"FUNDING_FEE","income":"-1","tranId":3,"time":%d},{"asset":"BNB","incomeType":"COMMISSION","income":"-5","tranId":4,"time":%d}]`, now, now, now, now)
		case "/fapi/v1/userTrades":
			if q.Get("startTime") == fmt.Sprint(from) {
				t.Fatal("sent 30d fills request")
			}
			return 200, fmt.Sprintf(`[{"id":1,"orderId":3,"price":"100","qty":"1","realizedPnl":"20","commission":"0.001","commissionAsset":"BNB","side":"SELL","positionSide":"LONG","time":%d}]`, now)
		default:
			t.Fatal(r.URL.Path)
			return 500, ""
		}
	})
	out := c.Analytics(context.Background(), "get_trading_history", AnalyticsRequest{Symbol: "BTCUSDT", Start: from, End: now, Limit: 1})
	b, _ := json.Marshal(out)
	if out.Status != "partial" || !strings.Contains(string(b), `"net":17`) || !strings.Contains(string(b), `"commissionAsset":"BNB"`) {
		t.Fatal(string(b))
	}
	fills := out.Data["fills"].(map[string]any)
	if fills["completeForIncomeWindow"] != false || fills["possiblyTruncated"] != true {
		t.Fatal(fills)
	}
	bad := analyticClient(t, func(r *http.Request) (int, string) { return 200, "null" })
	out = bad.Analytics(context.Background(), "get_trading_history", AnalyticsRequest{Symbol: "BTCUSDT"})
	if out.Status != "unavailable" || len(out.Data) > 0 {
		t.Fatal(out)
	}
}
func TestAnalyticsIndicatorCursorAndCandleClosed(t *testing.T) {
	c := analyticClient(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("endTime") != "1790696039999" {
			t.Fatal("cursor changed")
		}
		return 200, `[[1790695980000,"10","12","9","11","20",1790696039999,"200",5,"10","100","0"]]`
	})
	out := c.Analytics(context.Background(), "get_indicators", AnalyticsRequest{Symbol: "BTCUSDT", Limit: 1, End: 1790696040})
	b, e := json.Marshal(out)
	if e != nil || out.Status != "ok" || !strings.Contains(string(b), `"nextEnd":1790695980`) || !strings.Contains(string(b), `"closed":true`) || !strings.Contains(string(b), `"macd":null`) {
		t.Fatal(string(b), e)
	}
}
