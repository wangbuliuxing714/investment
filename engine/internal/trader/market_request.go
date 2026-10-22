package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"desk/internal/binance"
	"desk/internal/model"
)

type marketReader interface {
	KlinesBefore(context.Context, string, string, int, int64) ([]model.Candle, error)
}

type marketRequest struct {
	Symbol   string `json:"symbol"`
	Interval string `json:"interval"`
	Limit    int    `json:"limit"`
	End      int64  `json:"end"`
}

func (a *agentState) market(ctx context.Context, args string) string {
	body := marketRequest{Interval: "15m", Limit: 200}
	decoder := json.NewDecoder(strings.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return "行情参数无效（支持 symbol、interval、limit、end）：" + err.Error()
	}
	if err := decoder.Decode(new(any)); err != io.EOF || strings.TrimSpace(args) == "null" {
		return "行情参数需要是一份 JSON 对象，不能包含额外内容"
	}
	if !binance.ValidInterval(body.Interval) {
		return "K 线周期格式无效；例如：" + strings.Join(binance.Intervals, "、") + "。具体是否支持由币安接口返回。"
	}
	if body.Limit < 1 || body.Limit > 1500 {
		return "币安每次请求支持 1 到 1500 根 K 线。需要更多历史时，请使用返回的 nextEnd 分页读取。"
	}
	if body.End < 0 {
		return "end 需要是非负的 Unix 秒或毫秒时间戳"
	}
	symbols := a.strategy.Coins
	if symbol := strings.ToUpper(strings.TrimSpace(body.Symbol)); symbol != "" {
		symbols = []string{symbol}
	}
	reader := a.marketSource
	if reader == nil {
		reader = a.client
	}
	var out strings.Builder
	for _, symbol := range symbols {
		if err := ctx.Err(); err != nil {
			return out.String() + err.Error()
		}
		candles, err := reader.KlinesBefore(ctx, symbol, body.Interval, body.Limit, body.End)
		if err != nil {
			fmt.Fprintf(&out, "%s 行情读取失败：%s\n", symbol, err)
			continue
		}
		if len(candles) == 0 {
			fmt.Fprintf(&out, "%s %s 没有更早的行情\n", symbol, body.Interval)
			continue
		}
		// Historical reads must not become the reference price for a new order.
		if body.End == 0 {
			if a.marks == nil {
				a.marks = map[string]float64{}
			}
			a.marks[symbol] = candles[len(candles)-1].Close
		}
		first, last := candles[0].Time, candles[len(candles)-1].Time
		// end in seconds is an exclusive cursor (converted to milliseconds - 1).
		// Pass the first open time itself, not first-1, to avoid skipping 1s bars.
		fmt.Fprintf(&out, "symbol=%s interval=%s requested=%d count=%d nextEnd=%d（Unix 秒；原值传回 end，继续读取本页之前的历史）\n", symbol, body.Interval, body.Limit, len(candles), first)
		query := "最新一页"
		if body.End > 0 {
			query = fmt.Sprintf("历史一页 end=%d；本页价格不是当前实时报价", body.End)
		}
		fmt.Fprintf(&out, "查询=%s；获取时间=%s\n", query, time.Now().UTC().Format(time.RFC3339))
		fmt.Fprintf(&out, "K线开盘时间范围 UTC：%s → %s；北京时间 UTC+08:00：%s → %s\n", candleTime(first, time.UTC), candleTime(last, time.UTC), candleTime(first, beijingTime), candleTime(last, beijingTime))
		fmt.Fprintf(&out, "本页仅返回所请求的 %s K线窗口，不代表全部历史或仅能查看今天。需要更早数据可增大 limit（每次最多 1500）、改变 interval 或将 nextEnd 传入 end 分页；周期和历史范围由你选择。\n", body.Interval)
		out.WriteString(formatCoin(coinPack{Symbol: symbol, Interval: body.Interval, Candles: candles}))
		out.WriteString("\n")
	}
	if out.Len() == 0 {
		return "没有行情；请指定 symbol"
	}
	return out.String()
}
