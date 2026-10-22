package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"desk/internal/model"

	gobinance "github.com/adshao/go-binance/v2/futures"
)

type openOrder struct {
	Type         string
	Side         string
	PositionSide string
	Price        string
	Stop         string
	Trigger      string
	Activate     string
	Reduce       bool
	Close        bool
	WorkingType  string
}

// ChartOrders reads resting futures orders for one symbol, including conditional stops.
func (c *Client) ChartOrders(ctx context.Context, symbol string) ([]model.Level, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil, fmt.Errorf("缺少币种")
	}
	classic, err := c.raw.NewListOpenOrdersService().Symbol(symbol).Do(ctx, gobinance.WithRecvWindow(10000))
	if err != nil {
		return nil, wrap(err)
	}
	body, err := c.signedGet(ctx, "/fapi/v1/openAlgoOrders", mapQuery("symbol", symbol))
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		trimmed = "[]"
	}
	if strings.HasPrefix(trimmed, "{") {
		var apiErr struct {
			Msg string `json:"msg"`
		}
		_ = json.Unmarshal(body, &apiErr)
		if apiErr.Msg == "" {
			apiErr.Msg = "条件单没有返回列表"
		}
		return nil, fmt.Errorf("币安：%s", apiErr.Msg)
	}
	var algos []struct {
		Symbol        string `json:"symbol"`
		OrderType     string `json:"orderType"`
		Side          string `json:"side"`
		PositionSide  string `json:"positionSide"`
		Price         string `json:"price"`
		TriggerPrice  string `json:"triggerPrice"`
		ActivatePrice string `json:"activatePrice"`
		ReduceOnly    bool   `json:"reduceOnly"`
		ClosePosition bool   `json:"closePosition"`
		WorkingType   string `json:"workingType"`
	}
	if err := json.Unmarshal([]byte(trimmed), &algos); err != nil {
		return nil, fmt.Errorf("币安条件单没有读懂")
	}
	rows := make([]openOrder, 0, len(classic)+len(algos))
	for _, order := range classic {
		if order.Symbol != "" && !strings.EqualFold(order.Symbol, symbol) {
			continue
		}
		rows = append(rows, openOrder{
			Type:         string(order.Type),
			Side:         string(order.Side),
			PositionSide: string(order.PositionSide),
			Price:        order.Price,
			Stop:         order.StopPrice,
			Activate:     order.ActivatePrice,
			Reduce:       order.ReduceOnly,
			Close:        order.ClosePosition,
			WorkingType:  string(order.WorkingType),
		})
	}
	for _, order := range algos {
		if order.Symbol != "" && !strings.EqualFold(order.Symbol, symbol) {
			continue
		}
		rows = append(rows, openOrder{
			Type:         order.OrderType,
			Side:         order.Side,
			PositionSide: order.PositionSide,
			Price:        order.Price,
			Trigger:      order.TriggerPrice,
			Activate:     order.ActivatePrice,
			Reduce:       order.ReduceOnly,
			Close:        order.ClosePosition,
			WorkingType:  order.WorkingType,
		})
	}
	return chartOrders(rows), nil
}

func mapQuery(key, value string) url.Values {
	return url.Values{key: {value}}
}

func chartOrders(rows []openOrder) []model.Level {
	out := make([]model.Level, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		kind := orderKind(row.Type)
		if kind == "" {
			continue
		}
		price := orderPrice(kind, row)
		if price <= 0 {
			continue
		}
		side := levelSide(row.PositionSide, row.Side, row.Reduce || row.Close)
		protective := kind != "order" && closesPosition(row.Side, row.PositionSide, row.Reduce, row.Close, side)
		key := kind + "|" + side + "|" + strconv.FormatFloat(price, 'f', -1, 64) + "|" + strconv.FormatBool(protective) + "|" + row.WorkingType
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, model.Level{Kind: kind, Side: side, Price: price, Protective: protective, WorkingType: row.WorkingType})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Side != out[j].Side {
			return out[i].Side < out[j].Side
		}
		return out[i].Price < out[j].Price
	})
	return out
}

func orderKind(orderType string) string {
	switch strings.ToUpper(strings.TrimSpace(orderType)) {
	case "STOP", "STOP_MARKET":
		return "stop"
	case "TAKE_PROFIT", "TAKE_PROFIT_MARKET":
		return "take"
	case "TRAILING_STOP_MARKET":
		return "trail"
	case "LIMIT", "LIMIT_MAKER":
		return "order"
	default:
		return ""
	}
}

func orderPrice(kind string, row openOrder) float64 {
	if kind == "trail" {
		return firstPositive(row.Activate, row.Trigger, row.Stop, row.Price)
	}
	if kind == "order" {
		return atof(row.Price)
	}
	return firstPositive(row.Trigger, row.Stop, row.Price)
}

func firstPositive(values ...string) float64 {
	for _, value := range values {
		number := atof(value)
		if number > 0 {
			return number
		}
	}
	return 0
}

func levelSide(positionSide, side string, reduce bool) string {
	switch strings.ToUpper(strings.TrimSpace(positionSide)) {
	case "LONG":
		return "long"
	case "SHORT":
		return "short"
	}
	sell := strings.EqualFold(side, "SELL")
	if reduce {
		if sell {
			return "long"
		}
		return "short"
	}
	if sell {
		return "short"
	}
	return "long"
}
