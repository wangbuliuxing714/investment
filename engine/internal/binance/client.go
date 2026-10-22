package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"desk/internal/model"

	gobinance "github.com/adshao/go-binance/v2/futures"
)

const (
	liveBase = "https://fapi.binance.com"
	demoBase = "https://demo-fapi.binance.com"
)

type Client struct {
	raw     *gobinance.Client
	testnet bool
	http    *http.Client

	mu       sync.Mutex
	symbols  map[string]*rules
	loadedAt time.Time
	hedge    *bool
	hedgeAt  time.Time
}

type rules struct {
	qtyStep     string
	priceStep   string
	minQty      float64
	minNotional float64
}

type Fill struct {
	OrderID  int64
	Status   string
	AvgPrice string
	Qty      string
}

func New(key, secret string, testnet bool) *Client {
	c := newClient(key, secret, testnet, &http.Client{Timeout: 20 * time.Second})
	c.syncTime()
	return c
}

func newClient(key, secret string, testnet bool, httpClient *http.Client) *Client {
	raw := gobinance.NewClient(strings.TrimSpace(key), strings.TrimSpace(secret))
	base := liveBase
	if testnet {
		base = demoBase
	}
	raw.BaseURL = base
	raw.HTTPClient = httpClient
	return &Client{raw: raw, testnet: testnet, http: httpClient, symbols: map[string]*rules{}}
}

func (c *Client) Testnet() bool { return c.testnet }

func (c *Client) syncTime() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.raw.BaseURL+"/fapi/v1/time", nil)
	if err != nil {
		return
	}
	res, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	var body struct {
		ServerTime int64 `json:"serverTime"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body) != nil || body.ServerTime == 0 {
		return
	}
	c.raw.TimeOffset = time.Now().UnixMilli() - body.ServerTime
}

func (c *Client) Account(ctx context.Context) (model.Account, error) {
	acc, err := c.raw.NewGetAccountService().Do(ctx, gobinance.WithRecvWindow(10000))
	if err != nil {
		return model.Account{}, wrap(err)
	}
	out := model.Account{
		Equity:     atof(acc.TotalMarginBalance),
		Wallet:     atof(acc.TotalWalletBalance),
		Available:  atof(acc.AvailableBalance),
		Unrealized: atof(acc.TotalUnrealizedProfit),
		Testnet:    c.testnet,
	}
	out.MarginUsed = out.Equity - out.Available
	if out.MarginUsed < 0 {
		out.MarginUsed = 0
	}
	return out, nil
}

func (c *Client) Positions(ctx context.Context) ([]model.Position, error) {
	rows, err := c.raw.NewGetPositionRiskService().Do(ctx, gobinance.WithRecvWindow(10000))
	if err != nil {
		return nil, wrap(err)
	}
	out := make([]model.Position, 0)
	for _, row := range rows {
		amt := atof(row.PositionAmt)
		if math.Abs(amt) < 1e-12 {
			continue
		}
		side := "long"
		if amt < 0 || strings.EqualFold(row.PositionSide, "SHORT") {
			side = "short"
		}
		if strings.EqualFold(row.PositionSide, "LONG") {
			side = "long"
		}
		lev, _ := strconv.Atoi(row.Leverage)
		out = append(out, model.Position{
			Symbol:      row.Symbol,
			Side:        side,
			Amount:      math.Abs(amt),
			Entry:       atof(row.EntryPrice),
			Mark:        atof(row.MarkPrice),
			PnL:         atof(row.UnRealizedProfit),
			Leverage:    lev,
			Notional:    math.Abs(atof(row.Notional)),
			Liquidation: atof(row.LiquidationPrice),
		})
	}
	return out, nil
}

func (c *Client) Klines(ctx context.Context, symbol, interval string, limit int) ([]model.Candle, error) {
	return c.KlinesBefore(ctx, symbol, interval, limit, 0)
}

// KlinesBefore returns candles ending at end. end is unix seconds, or unix milliseconds when it is already 13 digits. Zero means the latest candles.
func (c *Client) KlinesBefore(ctx context.Context, symbol, interval string, limit int, end int64) ([]model.Candle, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 1500 {
		limit = 1500
	}
	svc := c.raw.NewKlinesService().Symbol(symbol).Interval(interval).Limit(limit)
	if ms := endMillis(end); ms > 0 {
		svc = svc.EndTime(ms)
	}
	rows, err := svc.Do(ctx)
	if err != nil {
		return nil, wrap(err)
	}
	out := make([]model.Candle, 0, len(rows))
	for _, row := range rows {
		closed := row.CloseTime < time.Now().UnixMilli()-c.raw.TimeOffset
		out = append(out, model.Candle{
			CloseTime: row.CloseTime,
			Closed:    &closed,
			Time:      row.OpenTime / 1000,
			Open:      atof(row.Open),
			High:      atof(row.High),
			Low:       atof(row.Low),
			Close:     atof(row.Close),
			Volume:    atof(row.Volume),
		})
	}
	return out, nil
}

func endMillis(end int64) int64 {
	if end <= 0 {
		return 0
	}
	if end < 1_000_000_000_000 {
		return end*1000 - 1
	}
	return end
}

func (c *Client) signedGet(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.signedRequest(ctx, http.MethodGet, path, query)
}

func (c *Client) signedRequest(ctx context.Context, method, path string, query url.Values) ([]byte, error) {
	if strings.TrimSpace(c.raw.APIKey) == "" || strings.TrimSpace(c.raw.SecretKey) == "" {
		return nil, fmt.Errorf("先在交易所页面保存币安密钥")
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("recvWindow", "10000")
	query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli()-c.raw.TimeOffset, 10))
	payload := query.Encode()
	mac := hmac.New(sha256.New, []byte(c.raw.SecretKey))
	_, _ = mac.Write([]byte(payload))
	full := c.raw.BaseURL + path + "?" + payload + "&signature=" + hex.EncodeToString(mac.Sum(nil))
	req, err := http.NewRequestWithContext(ctx, method, full, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.raw.APIKey)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, wrap(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, wrap(err)
	}
	if res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		var apiErr struct {
			Msg string `json:"msg"`
		}
		if json.Unmarshal(body, &apiErr) == nil && strings.TrimSpace(apiErr.Msg) != "" {
			msg = apiErr.Msg
		}
		return nil, wrap(fmt.Errorf("%s", msg))
	}
	return body, nil
}

func (c *Client) Hedge(ctx context.Context) (bool, error) {
	c.mu.Lock()
	if c.hedge != nil && time.Since(c.hedgeAt) < 5*time.Minute {
		v := *c.hedge
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()
	mode, err := c.raw.NewGetPositionModeService().Do(ctx, gobinance.WithRecvWindow(10000))
	if err != nil {
		return false, wrap(err)
	}
	c.mu.Lock()
	c.hedge = &mode.DualSidePosition
	c.hedgeAt = time.Now()
	c.mu.Unlock()
	return mode.DualSidePosition, nil
}

func (c *Client) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	_, err := c.raw.NewChangeLeverageService().Symbol(symbol).Leverage(leverage).Do(ctx, gobinance.WithRecvWindow(10000))
	return wrap(err)
}

func (c *Client) CancelAll(ctx context.Context, symbol string) error {
	return wrap(c.raw.NewCancelAllOpenOrdersService().Symbol(symbol).Do(ctx, gobinance.WithRecvWindow(10000)))
}

type MarketOrder struct {
	Symbol string
	Side   string
	Qty    string
	Hedge  bool
	Reduce bool
}

func (c *Client) Market(ctx context.Context, order MarketOrder) (Fill, error) {
	side := orderSide(order.Side)
	if order.Reduce {
		// Closing a long sells; closing a short buys, in either position mode.
		side = closeSide(order.Side)
	}
	svc := c.raw.NewCreateOrderService().
		Symbol(order.Symbol).
		Side(side).
		Type(gobinance.OrderTypeMarket).
		Quantity(order.Qty).
		NewOrderResponseType(gobinance.NewOrderRespTypeRESULT)
	if order.Hedge {
		svc.PositionSide(positionSide(order.Side))
	} else if order.Reduce {
		svc.ReduceOnly(true)
	}
	res, err := svc.Do(ctx, gobinance.WithRecvWindow(10000))
	if err != nil {
		return Fill{}, wrap(err)
	}
	return Fill{OrderID: res.OrderID, Status: string(res.Status), AvgPrice: res.AvgPrice, Qty: res.ExecutedQuantity}, nil
}

type StopOrder struct {
	Symbol string
	Side   string
	Kind   string
	Price  string
	Hedge  bool
}

func (c *Client) Stop(ctx context.Context, order StopOrder) error {
	if order.Side != "long" && order.Side != "short" {
		return fmt.Errorf("止盈止损方向无效")
	}
	price, err := strconv.ParseFloat(order.Price, 64)
	if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return fmt.Errorf("止盈止损触发价无效")
	}
	kind := "STOP_MARKET"
	if order.Kind == "take_profit" {
		kind = "TAKE_PROFIT_MARKET"
	} else if order.Kind != "" && order.Kind != "stop" {
		return fmt.Errorf("止盈止损类型无效")
	}
	// Conditional futures orders use the Algo API, not /fapi/v1/order (-4120).
	// closePosition must not be combined with quantity or reduceOnly.
	query := url.Values{
		"algoType": {"CONDITIONAL"}, "symbol": {order.Symbol},
		"side": {string(closeSide(order.Side))}, "type": {kind},
		"triggerPrice": {order.Price}, "closePosition": {"true"}, "workingType": {"MARK_PRICE"},
	}
	if order.Hedge {
		query.Set("positionSide", string(positionSide(order.Side)))
	}
	body, err := c.signedRequest(ctx, http.MethodPost, "/fapi/v1/algoOrder", query)
	if err != nil {
		return err
	}
	var ack struct {
		AlgoID     int64  `json:"algoId"`
		AlgoStatus string `json:"algoStatus"`
	}
	if json.Unmarshal(body, &ack) != nil || ack.AlgoID <= 0 || (ack.AlgoStatus != "NEW" && ack.AlgoStatus != "") {
		return fmt.Errorf("条件单未返回有效的接收确认，请先查询交易所条件单，勿重复开仓")
	}
	return nil
}

func (c *Client) Rules(ctx context.Context, symbol string) (qtyStep, priceStep string, minQty, minNotional float64, err error) {
	if err = c.loadRules(ctx); err != nil {
		return "", "", 0, 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rule, ok := c.symbols[symbol]
	if !ok {
		return "", "", 0, 0, fmt.Errorf("币安没有 %s 这个 U 本位合约", symbol)
	}
	return rule.qtyStep, rule.priceStep, rule.minQty, rule.minNotional, nil
}

func (c *Client) Symbols(ctx context.Context) ([]string, error) {
	if err := c.loadRules(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.symbols))
	for name := range c.symbols {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (c *Client) loadRules(ctx context.Context) error {
	c.mu.Lock()
	fresh := time.Since(c.loadedAt) < time.Hour && len(c.symbols) > 0
	c.mu.Unlock()
	if fresh {
		return nil
	}
	info, err := c.raw.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return wrap(err)
	}
	next := make(map[string]*rules, len(info.Symbols))
	for i := range info.Symbols {
		sym := &info.Symbols[i]
		if sym.QuoteAsset != "" && sym.QuoteAsset != "USDT" {
			continue
		}
		if sym.ContractType != "" && sym.ContractType != gobinance.ContractTypePerpetual {
			continue
		}
		if sym.Status != "" && sym.Status != "TRADING" {
			continue
		}
		next[sym.Symbol] = parseRules(sym.Filters)
	}
	c.mu.Lock()
	c.symbols = next
	c.loadedAt = time.Now()
	c.mu.Unlock()
	return nil
}

func parseRules(filters []map[string]interface{}) *rules {
	rule := &rules{qtyStep: "0.001", priceStep: "0.01", minQty: 0, minNotional: 5}
	for _, filter := range filters {
		kind, _ := filter["filterType"].(string)
		switch kind {
		case "LOT_SIZE", "MARKET_LOT_SIZE":
			if step, ok := filter["stepSize"].(string); ok && step != "" {
				rule.qtyStep = step
			}
			if kind == "LOT_SIZE" {
				if v, ok := filter["minQty"].(string); ok {
					rule.minQty = atof(v)
				}
			}
		case "PRICE_FILTER":
			if step, ok := filter["tickSize"].(string); ok && step != "" {
				rule.priceStep = step
			}
		case "MIN_NOTIONAL", "NOTIONAL":
			if v, ok := filter["notional"].(string); ok && atof(v) > 0 {
				rule.minNotional = atof(v)
			}
		}
	}
	return rule
}

func orderSide(side string) gobinance.SideType {
	if side == "short" {
		return gobinance.SideTypeSell
	}
	return gobinance.SideTypeBuy
}

func closeSide(side string) gobinance.SideType {
	if side == "short" {
		return gobinance.SideTypeBuy
	}
	return gobinance.SideTypeSell
}

func positionSide(side string) gobinance.PositionSideType {
	if side == "short" {
		return gobinance.PositionSideTypeShort
	}
	return gobinance.PositionSideTypeLong
}

func atof(v string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return f
}
