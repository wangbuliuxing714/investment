package trader

import (
	"context"
	"desk/internal/binance"
	"desk/internal/model"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
)

type ProtectionRequest struct {
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	StopLoss   float64 `json:"stop_loss"`
	TakeProfit float64 `json:"take_profit"`
}
type ProtectionResult struct {
	StopLoss   float64 `json:"stopLoss"`
	TakeProfit float64 `json:"takeProfit"`
	Confirmed  bool    `json:"confirmed"`
	Message    string  `json:"message"`
}
type protectionClient interface {
	Positions(context.Context) ([]model.Position, error)
	ChartOrders(context.Context, string) ([]model.Level, error)
	Hedge(context.Context) (bool, error)
	Rules(context.Context, string) (string, string, float64, float64, error)
	Stop(context.Context, binance.StopOrder) error
}

// Both the manual endpoint and agent tool use this gate. Re-read exchange
// orders inside it so simultaneous repairs cannot create duplicate protection.
var protectionGate = make(chan struct{}, 1)

func DecodeProtection(body io.Reader) (ProtectionRequest, error) {
	var req ProtectionRequest
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return req, fmt.Errorf("只接受一份保护单参数")
	}
	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	req.Side = strings.ToLower(strings.TrimSpace(req.Side))
	if req.Symbol == "" || (req.Side != "long" && req.Side != "short") {
		return req, fmt.Errorf("请指定合约和持仓方向")
	}
	if req.StopLoss < 0 || req.TakeProfit < 0 || math.IsNaN(req.StopLoss) || math.IsNaN(req.TakeProfit) || math.IsInf(req.StopLoss, 0) || math.IsInf(req.TakeProfit, 0) || (req.StopLoss == 0 && req.TakeProfit == 0) {
		return req, fmt.Errorf("请填写至少一个有效的止盈或止损价")
	}
	return req, nil
}

// Add missing exchange protection only; do not reopen, resize or silently
// replace a position's existing protective orders.
func ProtectPosition(ctx context.Context, client protectionClient, req ProtectionRequest) (ProtectionResult, error) {
	var result ProtectionResult
	select {
	case protectionGate <- struct{}{}:
		defer func() { <-protectionGate }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	positions, err := client.Positions(ctx)
	if err != nil {
		return result, err
	}
	pos := findPosition(positions, req.Symbol, req.Side)
	if pos == nil || pos.Mark <= 0 || math.IsNaN(pos.Mark) || math.IsInf(pos.Mark, 0) {
		return result, fmt.Errorf("没有该方向的持仓或有效标记价，未下单")
	}
	existing, err := client.ChartOrders(ctx, req.Symbol)
	if err != nil {
		return result, fmt.Errorf("无法核对已有保护单，未重复提交：%w", err)
	}
	apply := func(rows []model.Level) {
		result.StopLoss, result.TakeProfit = 0, 0
		for _, line := range rows {
			if !line.Protective || line.Side != req.Side {
				continue
			}
			if line.Kind == "stop" {
				result.StopLoss = line.Price
			}
			if line.Kind == "take" {
				result.TakeProfit = line.Price
			}
		}
	}
	apply(existing)
	_, step, _, _, err := client.Rules(ctx, req.Symbol)
	if err != nil {
		return result, err
	}
	stop, take := 0.0, 0.0
	if result.StopLoss == 0 && req.StopLoss > 0 {
		stop = atof(FloorToStep(req.StopLoss, step))
		if stop <= 0 || (req.Side == "long" && stop >= pos.Mark) || (req.Side == "short" && stop <= pos.Mark) {
			return result, fmt.Errorf("止损价必须位于当前标记价的止损方向")
		}
	}
	if result.TakeProfit == 0 && req.TakeProfit > 0 {
		take = atof(FloorToStep(req.TakeProfit, step))
		if take <= 0 || (req.Side == "long" && take <= pos.Mark) || (req.Side == "short" && take >= pos.Mark) {
			return result, fmt.Errorf("止盈价必须位于当前标记价的盈利方向")
		}
	}
	hedge, err := client.Hedge(ctx)
	if err != nil {
		return result, err
	}
	var messages []string
	if result.StopLoss > 0 {
		messages = append(messages, "保留交易所已有止损")
	}
	if result.TakeProfit > 0 {
		messages = append(messages, "保留交易所已有止盈")
	}
	for _, item := range []struct {
		kind, label string
		price       float64
	}{{"stop", "止损", stop}, {"take_profit", "止盈", take}} {
		if item.price <= 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			messages = append(messages, err.Error())
			break
		}
		if err := client.Stop(ctx, binance.StopOrder{Symbol: req.Symbol, Side: req.Side, Kind: item.kind, Price: FloorToStep(item.price, step), Hedge: hedge}); err != nil {
			messages = append(messages, item.label+"提交失败或待确认："+err.Error())
		} else {
			messages = append(messages, item.label+"请求已被交易所接收")
		}
	}
	rows, err := client.ChartOrders(ctx, req.Symbol)
	if err != nil {
		result.Confirmed = false
		messages = append(messages, "重新查询保护单失败，请刷新核对，勿重复开仓")
	} else {
		apply(rows)
		result.Confirmed = (req.StopLoss <= 0 || result.StopLoss > 0) && (req.TakeProfit <= 0 || result.TakeProfit > 0)
		if result.Confirmed {
			messages = append(messages, "已查询确认保护单；以各条件单的触发规则为准")
		} else {
			messages = append(messages, "保护尚未完整生效；程序没有本地自动止盈止损兜底")
		}
	}
	result.Message = strings.Join(messages, "；")
	return result, nil
}

func (a *agentState) protect(ctx context.Context, args string) string {
	if reason := a.modeDenial("set_protection", args); reason != "" {
		return reason
	}
	if a.paper {
		return "配置器处于模拟不发单模式，未发送保护单"
	}
	req, err := DecodeProtection(strings.NewReader(args))
	if err != nil {
		return err.Error()
	}
	if !allowedSymbol(a.strategy, req.Symbol) {
		return "该合约未在策略中启用，未发送保护单"
	}
	client, ok := a.client.(protectionClient)
	if !ok {
		return "当前连接不支持保护单查询和设置"
	}
	result, err := ProtectPosition(ctx, client, req)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("%s %s：%s；交易所止损=%g 止盈=%g（0 表示未查到）", req.Symbol, req.Side, result.Message, result.StopLoss, result.TakeProfit)
}
