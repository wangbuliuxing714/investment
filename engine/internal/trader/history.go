package trader

import (
	"context"
	"encoding/json"

	"desk/internal/history"
)

func (a *agentState) localHistory(ctx context.Context, args string) string {
	if a.service == nil || a.service.history == nil {
		return `{"error":"本地历史库尚未开启","executed":false}`
	}
	var q struct {
		Market   string `json:"market"`
		Symbol   string `json:"symbol"`
		Interval string `json:"interval"`
		End      int64  `json:"end"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(args), &q); err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b)
	}
	if q.Symbol == "" {
		q.Symbol = "BTCUSDT"
	}
	if q.Interval == "" {
		q.Interval = "1d"
	}
	if q.Limit == 0 {
		q.Limit = 200
	}
	p, err := a.service.history.Query(ctx, q.Market, q.Symbol, q.Interval, q.End, q.Limit)
	if err != nil {
		b, _ := json.Marshal(map[string]any{"error": err.Error(), "request": q})
		return string(b)
	}
	b, err := json.Marshal(struct {
		history.Page
		Request          any  `json:"request"`
		Count            int  `json:"count"`
		ResponseComplete bool `json:"responseComplete"`
	}{p, q, len(p.Candles), true})
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b)
	}
	return string(b)
}
