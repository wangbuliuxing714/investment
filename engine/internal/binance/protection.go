package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"desk/internal/model"
	gobinance "github.com/adshao/go-binance/v2/futures"
)

type algoOrder struct {
	AlgoID        int64  `json:"algoId"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	PositionSide  string `json:"positionSide"`
	OrderType     string `json:"orderType"`
	ClosePosition bool   `json:"closePosition"`
	ReduceOnly    bool   `json:"reduceOnly"`
}

func closesPosition(side, positionSide string, reduce, closeAll bool, wanted string) bool {
	if side != string(closeSide(wanted)) {
		return false
	}
	if positionSide == "LONG" || positionSide == "SHORT" {
		return strings.EqualFold(positionSide, wanted)
	}
	return reduce || closeAll
}

// Remove only closing conditionals for this position, including Algo orders.
// Never remove the opposite hedge's protection or unrelated opening orders.
func (c *Client) CancelProtection(ctx context.Context, symbol, side string) error {
	classic, err := c.raw.NewListOpenOrdersService().Symbol(symbol).Do(ctx, gobinance.WithRecvWindow(10000))
	if err != nil {
		return wrap(err)
	}
	body, err := c.signedGet(ctx, "/fapi/v1/openAlgoOrders", mapQuery("symbol", symbol))
	if err != nil {
		return err
	}
	var algos []algoOrder
	if err = json.Unmarshal(body, &algos); err != nil {
		return fmt.Errorf("条件单列表无法解析：%w", err)
	}
	var failures []error
	for _, order := range classic {
		kind := orderKind(string(order.Type))
		if kind == "" || kind == "order" || !closesPosition(string(order.Side), string(order.PositionSide), order.ReduceOnly, order.ClosePosition, side) {
			continue
		}
		q := mapQuery("symbol", symbol)
		q.Set("orderId", strconv.FormatInt(order.OrderID, 10))
		if _, err = c.signedRequest(ctx, http.MethodDelete, "/fapi/v1/order", q); err != nil {
			failures = append(failures, err)
		}
	}
	for _, order := range algos {
		if order.Symbol != "" && !strings.EqualFold(order.Symbol, symbol) {
			continue
		}
		if order.AlgoID <= 0 || !closesPosition(order.Side, order.PositionSide, order.ReduceOnly, order.ClosePosition, side) {
			continue
		}
		if _, err = c.signedRequest(ctx, http.MethodDelete, "/fapi/v1/algoOrder", mapQuery("algoId", strconv.FormatInt(order.AlgoID, 10))); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (c *Client) PositionsWithProtection(ctx context.Context) ([]model.Position, error) {
	positions, err := c.Positions(ctx)
	if err != nil {
		return nil, err
	}
	type found struct {
		levels []model.Level
		err    error
	}
	results := map[string]found{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for _, p := range positions {
		results[p.Symbol] = found{}
	}
	symbols := make([]string, 0, len(results))
	for symbol := range results {
		symbols = append(symbols, symbol)
	}
	for _, symbol := range symbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				results[symbol] = found{err: ctx.Err()}
				mu.Unlock()
				return
			}
			defer func() { <-slots }()
			rows, err := c.ChartOrders(ctx, symbol)
			mu.Lock()
			results[symbol] = found{rows, err}
			mu.Unlock()
		}(symbol)
	}
	wg.Wait()
	for i := range positions {
		r := results[positions[i].Symbol]
		positions[i].ProtectionKnown = r.err == nil
		if r.err != nil {
			positions[i].ProtectionError = r.err.Error()
			continue
		}
		for _, line := range r.levels {
			if line.Protective && line.Side == positions[i].Side {
				positions[i].Protection = append(positions[i].Protection, line)
			}
		}
	}
	return positions, nil
}
