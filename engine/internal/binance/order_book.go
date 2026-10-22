package binance

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
)

type BookLevel struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
}
type SlippageEstimate struct {
	RequestedQuantity float64  `json:"requestedQuantity"`
	FilledQuantity    float64  `json:"filledQuantity"`
	FullyCovered      bool     `json:"fullyCovered"`
	FilledVWAP        *float64 `json:"filledVWAP"`
	FullOrderVWAP     *float64 `json:"fullOrderVWAP"`
	SlippageBps       *float64 `json:"slippageBps"`
}

func (c *Client) orderBook(ctx context.Context, symbol string, limit int, notional float64) (any, error) {
	raw, e := c.publicObject(ctx, "/fapi/v1/depth", url.Values{"symbol": {symbol}, "limit": {strconv.Itoa(limit)}}, []string{"lastUpdateId", "E", "T"})
	if e != nil {
		return nil, e
	}
	bids, e := decodeLevels(raw["bids"], true)
	if e != nil {
		return nil, e
	}
	asks, e := decodeLevels(raw["asks"], false)
	if e != nil {
		return nil, e
	}
	if len(bids) == 0 || len(asks) == 0 {
		return nil, fmt.Errorf("盘口缺少买方或卖方档位")
	}
	bid, ask := bids[0].Price, asks[0].Price
	if bid >= ask {
		return nil, fmt.Errorf("盘口交叉或锁定，不能估算滑点")
	}
	mid := (bid + ask) / 2
	bq, bn := bookDepth(bids)
	aq, an := bookDepth(asks)
	out := map[string]any{"lastUpdateId": raw["lastUpdateId"], "eventTime": raw["E"], "transactionTime": raw["T"], "bids": bids, "asks": asks, "bestBid": bid, "bestAsk": ask, "mid": mid, "spread": ask - bid, "spreadBps": (ask - bid) / mid * 10000, "visibleBidQuantity": bq, "visibleAskQuantity": aq, "visibleBidNotional": bn, "visibleAskNotional": an, "depthImbalance": (bq - aq) / (bq + aq)}
	if notional > 0 {
		qty := notional / mid
		out["notionalUSD"] = notional
		out["buyEstimate"] = estimateSlippage(asks, qty, mid, true)
		out["sellEstimate"] = estimateSlippage(bids, qty, mid, false)
	}
	return out, nil
}
func decodeLevels(v any, descending bool) ([]BookLevel, error) {
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("盘口档位格式无效")
	}
	out := make([]BookLevel, 0, len(raw))
	seen := map[float64]bool{}
	for _, v := range raw {
		row, ok := v.([]any)
		if !ok || len(row) != 2 {
			return nil, fmt.Errorf("盘口价量格式无效")
		}
		p, e1 := number(row[0])
		q, e2 := number(row[1])
		if e1 != nil || e2 != nil || p <= 0 || q < 0 || seen[p] {
			return nil, fmt.Errorf("盘口价量无效或价格重复")
		}
		seen[p] = true
		if q > 0 {
			out = append(out, BookLevel{p, q})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if descending {
			return out[i].Price > out[j].Price
		}
		return out[i].Price < out[j].Price
	})
	return out, nil
}
func bookDepth(rows []BookLevel) (qty, notional float64) {
	for _, r := range rows {
		qty += r.Quantity
		notional += r.Price * r.Quantity
	}
	return
}
func estimateSlippage(rows []BookLevel, qty, mid float64, buy bool) SlippageEstimate {
	out := SlippageEstimate{RequestedQuantity: qty}
	remaining := qty
	cost := 0.0
	for _, r := range rows {
		q := min(remaining, r.Quantity)
		cost += q * r.Price
		out.FilledQuantity += q
		remaining -= q
		if remaining <= 0 {
			break
		}
	}
	out.FullyCovered = remaining <= qty*1e-12
	if out.FilledQuantity > 0 {
		v := cost / out.FilledQuantity
		out.FilledVWAP = &v
		if out.FullyCovered {
			out.FullOrderVWAP = &v
			slip := (v - mid) / mid * 10000
			if !buy {
				slip = -slip
			}
			out.SlippageBps = &slip
		}
	}
	return out
}
