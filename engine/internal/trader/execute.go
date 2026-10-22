package trader

import (
	"context"
	"fmt"
	"strings"

	"desk/internal/binance"
	"desk/internal/model"
)

type executionClient interface {
	Rules(context.Context, string) (string, string, float64, float64, error)
	CancelProtection(context.Context, string, string) error
	Market(context.Context, binance.MarketOrder) (binance.Fill, error)
	SetLeverage(context.Context, string, int) error
	Stop(context.Context, binance.StopOrder) error
}

func executePlan(ctx context.Context, client executionClient, hedge bool, positions []model.Position, mark float64, plan Plan, paper bool) (string, []model.Position, bool) {
	if mark <= 0 {
		mark = markOf(positions, plan.Decision.Symbol)
	}
	if plan.Skip != "" {
		return plan.Decision.Symbol + " " + actionLabel(plan.Decision.Action) + "：" + plan.Skip, positions, false
	}
	if paper && tradable(plan.Decision.Action) {
		return plan.Decision.Symbol + " " + actionLabel(plan.Decision.Action) + "：配置器处于模拟交易，没有向币安发单", positions, false
	}
	var lines []string
	lines = append(lines, plan.Decision.Symbol+" "+actionLabel(plan.Decision.Action))
	for _, note := range plan.Notes {
		lines = append(lines, "  "+note)
	}
	switch plan.Decision.Action {
	case "close_long", "close_short":
		side := "long"
		if plan.Decision.Action == "close_short" {
			side = "short"
		}
		pos := findPosition(positions, plan.Decision.Symbol, side)
		if pos == nil {
			lines = append(lines, "  最新持仓中没有可平的仓，未向币安发送平仓订单")
			return strings.Join(lines, "\n"), positions, false
		}
		qtyStep, _, minQty, _, err := client.Rules(ctx, plan.Decision.Symbol)
		if err != nil {
			lines = append(lines, "  "+err.Error())
			return strings.Join(lines, "\n"), positions, false
		}
		qty := FloorToStep(pos.Amount, qtyStep)
		if qty == "0" || atof(qty) < minQty {
			lines = append(lines, "  持仓数量低于交易所最小下单量")
			return strings.Join(lines, "\n"), positions, false
		}
		fill, err := client.Market(ctx, binance.MarketOrder{Symbol: plan.Decision.Symbol, Side: side, Qty: qty, Hedge: hedge, Reduce: true})
		if err != nil {
			lines = append(lines, "  平仓失败："+err.Error())
			return strings.Join(lines, "\n"), positions, false
		}
		lines = append(lines, fmt.Sprintf("  已平仓，订单 %d，成交量 %s，均价 %s", fill.OrderID, fill.Qty, blank(fill.AvgPrice)))
		if err := client.CancelProtection(ctx, plan.Decision.Symbol, side); err != nil {
			lines = append(lines, "  平仓已成交，但清理该方向旧保护单失败："+err.Error())
		}
		return strings.Join(lines, "\n"), dropPosition(positions, plan.Decision.Symbol, side), true
	case "open_long", "open_short":
		side := "long"
		if plan.Decision.Action == "open_short" {
			side = "short"
		}
		qtyStep, priceStep, minQty, minNotional, err := client.Rules(ctx, plan.Decision.Symbol)
		if err != nil {
			lines = append(lines, "  "+err.Error())
			return strings.Join(lines, "\n"), positions, false
		}
		if plan.Notional < minNotional {
			lines = append(lines, fmt.Sprintf("  名义价值 %.2f 低于交易所最小 %.2f，未下单", plan.Notional, minNotional))
			return strings.Join(lines, "\n"), positions, false
		}
		if mark <= 0 {
			lines = append(lines, "  没有最新价，未下单")
			return strings.Join(lines, "\n"), positions, false
		}
		qty := FloorToStep(plan.Notional/mark, qtyStep)
		if qty == "0" || atof(qty) < minQty {
			lines = append(lines, "  换算后的数量低于最小下单量，未下单")
			return strings.Join(lines, "\n"), positions, false
		}
		if err := client.SetLeverage(ctx, plan.Decision.Symbol, plan.Leverage); err != nil {
			lines = append(lines, "  设置杠杆失败："+err.Error())
			return strings.Join(lines, "\n"), positions, false
		}
		fill, err := client.Market(ctx, binance.MarketOrder{Symbol: plan.Decision.Symbol, Side: side, Qty: qty, Hedge: hedge})
		if err != nil {
			lines = append(lines, "  开仓失败："+err.Error())
			return strings.Join(lines, "\n"), positions, false
		}
		lines = append(lines, fmt.Sprintf("  已开仓，订单 %d，数量 %s，均价 %s，杠杆 %d", fill.OrderID, fill.Qty, blank(fill.AvgPrice), plan.Leverage))
		if err := client.CancelProtection(ctx, plan.Decision.Symbol, side); err != nil {
			lines = append(lines, "  清理旧条件单失败："+err.Error())
		}
		placeStop(ctx, client, hedge, side, plan, priceStep, &lines)
		positions = append(positions, model.Position{Symbol: plan.Decision.Symbol, Side: side, Amount: atof(qty), Leverage: plan.Leverage})
		return strings.Join(lines, "\n"), positions, true
	default:
		lines = append(lines, "  未执行")
		return strings.Join(lines, "\n"), positions, false
	}
}

func placeStop(ctx context.Context, client executionClient, hedge bool, side string, plan Plan, priceStep string, lines *[]string) {
	failed := false
	if plan.Decision.StopLoss > 0 {
		price := FloorToStep(plan.Decision.StopLoss, priceStep)
		if err := client.Stop(ctx, binance.StopOrder{Symbol: plan.Decision.Symbol, Side: side, Kind: "stop", Price: price, Hedge: hedge}); err != nil {
			failed = true
			*lines = append(*lines, "  止损未挂上："+err.Error())
		} else {
			*lines = append(*lines, "  止损 "+price)
		}
	}
	if plan.Decision.TakeProfit > 0 {
		price := FloorToStep(plan.Decision.TakeProfit, priceStep)
		if err := client.Stop(ctx, binance.StopOrder{Symbol: plan.Decision.Symbol, Side: side, Kind: "take_profit", Price: price, Hedge: hedge}); err != nil {
			failed = true
			*lines = append(*lines, "  止盈未挂上："+err.Error())
		} else {
			*lines = append(*lines, "  止盈 "+price)
		}
	}
	if failed {
		*lines = append(*lines, "  保护单未完整生效：开仓成交不代表止盈止损已设置。程序没有本地自动止盈止损兜底；请核对交易所条件单并补挂，勿重复开仓。")
	}
}

func tradable(action string) bool {
	return action == "open_long" || action == "open_short" || action == "close_long" || action == "close_short"
}

func findPosition(positions []model.Position, symbol, side string) *model.Position {
	for i := range positions {
		if positions[i].Symbol == symbol && positions[i].Side == side && positions[i].Amount > 0 {
			return &positions[i]
		}
	}
	return nil
}

func dropPosition(positions []model.Position, symbol, side string) []model.Position {
	out := make([]model.Position, 0, len(positions))
	for _, p := range positions {
		if p.Symbol == symbol && p.Side == side {
			continue
		}
		out = append(out, p)
	}
	return out
}

func markOf(positions []model.Position, symbol string) float64 {
	for _, p := range positions {
		if p.Symbol == symbol && p.Mark > 0 {
			return p.Mark
		}
	}
	return 0
}

func blank(v string) string {
	if strings.TrimSpace(v) == "" || v == "0" || v == "0.00000" {
		return "市价"
	}
	return v
}

func atof(v string) float64 {
	var f float64
	fmt.Sscan(v, &f)
	return f
}
