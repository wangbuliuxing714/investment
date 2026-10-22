package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"desk/internal/binance"
	"desk/internal/model"

	"github.com/coder/websocket"
)

func (s *Server) klineLive(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	interval := strings.TrimSpace(r.URL.Query().Get("interval"))
	if symbol == "" {
		writeErr(w, http.StatusBadRequest, errors.New("缺少币种"))
		return
	}
	if !allowedFrame(interval) {
		writeErr(w, http.StatusBadRequest, errors.New("K 线周期格式无效"))
		return
	}
	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer client.Close(websocket.StatusNormalClosure, "")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		defer cancel()
		for {
			if _, _, err := client.Read(ctx); err != nil {
				return
			}
		}
	}()
	demo := s.creds().Demo
	upstream := klineStreamURL(demo, symbol, interval)
	for ctx.Err() == nil {
		// Keep the successful dial context alive for the life of the connection.
		dialCtx, stop := context.WithCancel(ctx)
		timer := time.AfterFunc(8*time.Second, stop)
		up, _, err := websocket.Dial(dialCtx, upstream, nil)
		timer.Stop()
		if err != nil {
			stop()
			if !sleepCtx(ctx, time.Second) {
				return
			}
			continue
		}
		forwardKlines(ctx, client, up)
		stop()
		_ = up.CloseNow()
		if !sleepCtx(ctx, time.Second) {
			return
		}
	}
}

func klineStreamURL(demo bool, symbol, interval string) string {
	host := "fstream.binance.com"
	if demo {
		host = "fstream.binancefuture.com"
	}
	return fmt.Sprintf("wss://%s/market/ws/%s@kline_%s", host, strings.ToLower(symbol), interval)
}

type klineReader interface {
	Klines(context.Context, string, string, int) ([]model.Candle, error)
}

func forwardDemoKlines(ctx context.Context, socket *websocket.Conn, source klineReader, symbol, interval string) {
	for ctx.Err() == nil {
		readCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		candles, err := source.Klines(readCtx, symbol, interval, 2)
		cancel()
		if err == nil && len(candles) > 0 {
			candles[len(candles)-1].Source = "rest"
			candles[len(candles)-1].UpdatedAt = time.Now().UnixMilli()
			body, err := json.Marshal(candles[len(candles)-1])
			if err != nil {
				return
			}
			writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			err = socket.Write(writeCtx, websocket.MessageText, body)
			stop()
			if err != nil {
				return
			}
		}
		if !sleepCtx(ctx, 2*time.Second) {
			return
		}
	}
}

func forwardKlines(ctx context.Context, client, up *websocket.Conn) {
	for {
		readCtx, stop := context.WithTimeout(ctx, 15*time.Second)
		_, msg, err := up.Read(readCtx)
		stop()
		if err != nil {
			return
		}
		candle, ok := parseLiveKline(msg)
		if !ok {
			continue
		}
		body, err := json.Marshal(candle)
		if err != nil {
			continue
		}
		writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = client.Write(writeCtx, websocket.MessageText, body)
		cancel()
		if err != nil {
			return
		}
	}
}

func parseLiveKline(raw []byte) (model.Candle, bool) {
	// Binance sends both t/T, l/L and v/V. encoding/json treats those as the same field.
	var event map[string]json.RawMessage
	if json.Unmarshal(raw, &event) != nil {
		return model.Candle{}, false
	}
	if len(event["data"]) > 0 {
		return parseLiveKline(event["data"])
	}
	var k map[string]json.RawMessage
	if json.Unmarshal(event["k"], &k) != nil {
		return model.Candle{}, false
	}
	eventTime, _ := jsonInt(event["E"])
	start, ok := jsonInt(k["t"])
	if !ok || start <= 0 {
		return model.Candle{}, false
	}
	open, ok1 := jsonFloat(k["o"])
	high, ok2 := jsonFloat(k["h"])
	low, ok3 := jsonFloat(k["l"])
	closePrice, ok4 := jsonFloat(k["c"])
	volume, ok5 := jsonFloat(k["v"])
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || open <= 0 || high <= 0 || low <= 0 || closePrice <= 0 || volume < 0 {
		return model.Candle{}, false
	}
	closeTime, _ := jsonInt(k["T"])
	var closed *bool
	_ = json.Unmarshal(k["x"], &closed)
	return model.Candle{
		CloseTime: closeTime,
		Closed:    closed,
		Time:      start / 1000,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePrice,
		Volume:    volume,
		UpdatedAt: eventTime,
		Source:    "websocket",
	}, true
}

func jsonInt(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var value int64
	if json.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	return value, true
}

func jsonFloat(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return parsePrice(text)
	}
	var value float64
	if json.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	return value, true
}

func parsePrice(text string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func allowedFrame(interval string) bool {
	return binance.ValidInterval(interval)
}
