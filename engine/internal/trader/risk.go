package trader

import (
	"fmt"
	"strings"

	"desk/internal/model"
)

// Plan is a decision after the program has applied hard limits.
type Plan struct {
	Decision model.Decision
	Notional float64
	Leverage int
	Notes    []string
	Skip     string
}

func Apply(st model.Strategy, equity, marginUsed float64, positions []model.Position, mark float64, d model.Decision) Plan {
	d.Symbol = strings.ToUpper(strings.TrimSpace(d.Symbol))
	plan := Plan{Decision: d, Leverage: d.Leverage, Notional: d.PositionSizeUSD}
	if d.Action == "" || d.Action == "hold" {
		plan.Skip = "观望"
		return plan
	}
	if !allowedSymbol(st, d.Symbol) {
		plan.Skip = d.Symbol + " 不在策略币种里"
		return plan
	}
	if d.Action == "close_long" || d.Action == "close_short" {
		return plan
	}
	if d.Action != "open_long" && d.Action != "open_short" {
		plan.Skip = "无法识别的动作"
		return plan
	}
	side := "long"
	if d.Action == "open_short" {
		side = "short"
	}
	openCount := 0
	for _, p := range positions {
		if p.Amount != 0 {
			openCount++
		}
		if p.Symbol == d.Symbol && p.Amount != 0 {
			if p.Side == side {
				plan.Skip = d.Symbol + " 已有同向持仓"
			} else {
				plan.Skip = d.Symbol + " 已有反向持仓，先平掉再开"
			}
			return plan
		}
	}
	if st.MaxPositions > 0 && openCount >= st.MaxPositions {
		plan.Skip = fmt.Sprintf("持仓已有 %d 个，达到上限", openCount)
		return plan
	}
	maxLev := st.AltLeverage
	if d.Symbol == "BTCUSDT" || d.Symbol == "ETHUSDT" {
		maxLev = st.BTCETHLeverage
	}
	if maxLev < 1 {
		maxLev = 1
	}
	if plan.Leverage < 1 || plan.Leverage > maxLev {
		plan.Notes = append(plan.Notes, fmt.Sprintf("杠杆从 %d 调整为 %d", plan.Leverage, maxLev))
		plan.Leverage = maxLev
	}
	if equity <= 0 {
		plan.Skip = "账户净值为 0"
		return plan
	}
	if plan.Notional <= 0 {
		plan.Skip = "模型没有给出仓位金额"
		return plan
	}
	capNotional := equity * st.PositionValueRatio
	if st.PositionValueRatio > 0 && plan.Notional > capNotional {
		plan.Notes = append(plan.Notes, fmt.Sprintf("单笔上限把名义价值从 %.2f 调到 %.2f", plan.Notional, capNotional))
		plan.Notional = capNotional
	}
	if st.MaxMarginUsage > 0 && plan.Leverage > 0 {
		room := equity*st.MaxMarginUsage - marginUsed
		if room <= 0 {
			plan.Skip = "保证金已到上限"
			return plan
		}
		maxByMargin := room * float64(plan.Leverage)
		if plan.Notional > maxByMargin {
			plan.Notes = append(plan.Notes, fmt.Sprintf("保证金上限把名义价值从 %.2f 调到 %.2f", plan.Notional, maxByMargin))
			plan.Notional = maxByMargin
		}
	}
	if mark > 0 && plan.Leverage > 0 {
		afford := (equity - marginUsed) * 0.98 * float64(plan.Leverage)
		if afford < plan.Notional {
			if afford <= 0 {
				plan.Skip = "可用保证金不足"
				return plan
			}
			plan.Notes = append(plan.Notes, fmt.Sprintf("可用保证金把名义价值从 %.2f 调到 %.2f", plan.Notional, afford))
			plan.Notional = afford
		}
	}
	if plan.Notional <= 0 {
		plan.Skip = "计算后的下单金额为 0"
		return plan
	}
	plan.Decision.StopLoss, plan.Decision.TakeProfit = validStops(d.Action, mark, d.StopLoss, d.TakeProfit, &plan.Notes)
	return plan
}

func validStops(action string, mark, stop, take float64, notes *[]string) (float64, float64) {
	if mark <= 0 {
		return stop, take
	}
	long := action == "open_long"
	if stop > 0 && ((long && stop >= mark) || (!long && stop <= mark)) {
		*notes = append(*notes, "止损方向不对，已忽略")
		stop = 0
	}
	if take > 0 && ((long && take <= mark) || (!long && take >= mark)) {
		*notes = append(*notes, "止盈方向不对，已忽略")
		take = 0
	}
	return stop, take
}

func allowedSymbol(st model.Strategy, symbol string) bool {
	for _, c := range st.Coins {
		if strings.EqualFold(c, symbol) {
			return true
		}
	}
	return false
}
