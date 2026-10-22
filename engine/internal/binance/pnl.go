package binance

import (
	"context"
	"desk/internal/model"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"time"
)

type incomeRow struct {
	Asset      string      `json:"asset"`
	IncomeType string      `json:"incomeType"`
	Income     string      `json:"income"`
	TranID     json.Number `json:"tranId"`
	Time       int64       `json:"time"`
}

// Query an explicit period; transfers/deposits are not trading PnL.
func (c *Client) IncomeSummary(ctx context.Context, from, to int64) (model.PnLSummary, error) {
	return c.incomeSummary(ctx, "", from, to)
}

func (c *Client) incomeSummary(ctx context.Context, symbol string, from, to int64) (model.PnLSummary, error) {
	out := model.PnLSummary{Asset: "USDT", From: from, To: to}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		q := url.Values{"startTime": {strconv.FormatInt(from, 10)}, "endTime": {strconv.FormatInt(to, 10)}, "limit": {"1000"}, "page": {strconv.Itoa(page)}}
		if symbol != "" {
			q.Set("symbol", symbol)
		}
		body, err := c.signedGet(ctx, "/fapi/v1/income", q)
		if err != nil {
			return out, err
		}
		var rows []incomeRow
		if err = json.Unmarshal(body, &rows); err != nil {
			return out, fmt.Errorf("已实现盈亏流水无法解析：%w", err)
		}
		if rows == nil {
			return out, fmt.Errorf("收益流水未返回有效数组")
		}
		added := 0
		for _, row := range rows {
			if row.TranID.String() == "" {
				return out, fmt.Errorf("流水缺少交易编号，未显示不完整汇总")
			}
			key := row.IncomeType + "/" + row.Asset + "/" + row.TranID.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			added++
			if row.Asset != "USDT" || row.Time < from || row.Time > to {
				continue
			}
			value, err := strconv.ParseFloat(row.Income, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return out, fmt.Errorf("流水金额无效")
			}
			switch row.IncomeType {
			case "REALIZED_PNL":
				out.Realized += value
			case "COMMISSION":
				out.Commission += value
			case "FUNDING_FEE":
				out.Funding += value
			}
		}
		if len(rows) < 1000 {
			break
		}
		if added == 0 {
			return out, fmt.Errorf("盈亏流水分页没有推进，未显示不完整汇总")
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
	}
	out.Net = out.Realized + out.Commission + out.Funding
	out.UpdatedAt = time.Now().UnixMilli()
	return out, nil
}
