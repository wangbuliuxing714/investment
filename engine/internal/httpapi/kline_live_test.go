package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"desk/internal/model"
	"github.com/coder/websocket"
)

type demoKlineFixture struct{ requests chan string }

func (f demoKlineFixture) Klines(_ context.Context, symbol, interval string, limit int) ([]model.Candle, error) {
	f.requests <- symbol + "/" + interval
	return []model.Candle{{Time: 100, Close: 10}, {Time: 160, Close: 20}}, nil
}

func TestDemoKlineStreamUsesDemoCandles(t *testing.T) {
	fixture := demoKlineFixture{requests: make(chan string, 4)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		forwardDemoKlines(conn.CloseRead(r.Context()), conn, fixture, "BTCUSDT", "1m")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var candle model.Candle
	if err := json.Unmarshal(data, &candle); err != nil || candle.Time != 160 || candle.Close != 20 {
		t.Fatal("latest demo candle was not delivered")
	}
	if got := <-fixture.requests; got != "BTCUSDT/1m" {
		t.Fatal(got)
	}
}

func TestParseLiveKline(t *testing.T) {
	raw := []byte(`{"e":"kline","s":"BTCUSDT","k":{"t":1790691120000,"o":"83872.2","c":"83834.8","h":"83872.2","l":"83834.8","v":"93.669","i":"1m"}}`)
	candle, ok := parseLiveKline(raw)
	if !ok {
		t.Fatal("expected a candle")
	}
	if candle.Time != 1790691120 || candle.Open != 83872.2 || candle.Close != 83834.8 || candle.Volume != 93.669 {
		t.Fatalf("%+v", candle)
	}
}

func TestParseLiveKlineRealFrame(t *testing.T) {
	raw := []byte(`{"e":"kline","E":1790691752365,"s":"BTCUSDT","k":{"t":1790691720000, "T":1790691779999, "s":"BTCUSDT", "i":"1m", "f":8130053979, "L":8130056023, "o":"83910.20", "c":"83895.20", "h":"83912.90", "l":"83866.40", "v":"40.664", "n":2040, "x":false, "q":"3411510.38310", "V":"30.011", "Q":"2517796.93340", "B":"0"}}`)
	candle, ok := parseLiveKline(raw)
	if !ok {
		t.Fatal("real frame was rejected")
	}
	if candle.Time != 1790691720 || candle.Close != 83895.2 {
		t.Fatalf("%+v", candle)
	}
}

func TestParseLiveKlineRejectsNoise(t *testing.T) {
	if _, ok := parseLiveKline([]byte(`{"result":null}`)); ok {
		t.Fatal("ack should be ignored")
	}
}
