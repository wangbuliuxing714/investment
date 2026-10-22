package binance

import (
	"bytes"
	"context"
	"desk/internal/market"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type AnalyticsRequest struct {
	Symbol      string  `json:"symbol"`
	Interval    string  `json:"interval,omitempty"`
	Period      string  `json:"period,omitempty"`
	Limit       int     `json:"limit,omitempty"`
	Start       int64   `json:"start,omitempty"`
	End         int64   `json:"end,omitempty"`
	Environment string  `json:"environment,omitempty"`
	NotionalUSD float64 `json:"notionalUSD,omitempty"`
}

type AnalyticsSource struct {
	Exchange           string `json:"exchange"`
	Environment        string `json:"environment"`
	Host               string `json:"host"`
	ReferenceOnly      bool   `json:"referenceOnly"`
	TradingEnvironment string `json:"tradingEnvironment"`
}

type AnalyticsResult struct {
	Tool      string            `json:"tool"`
	Status    string            `json:"status"`
	Source    AnalyticsSource   `json:"source"`
	FetchedAt string            `json:"fetchedAt"`
	Request   AnalyticsRequest  `json:"request"`
	Data      map[string]any    `json:"data"`
	Errors    map[string]string `json:"errors,omitempty"`
	Notes     []string          `json:"notes"`
}

func IsAnalyticsTool(name string) bool {
	switch name {
	case "get_indicators", "get_trading_history", "get_funding", "get_open_interest", "get_order_flow", "get_order_book":
		return true
	}
	return false
}

// Account/execution always stays on the configured client. OI/order-flow are
// market statistics: in demo mode their omitted source defaults to public live
// reference data. Explicit active still means demo; failures never change source.
func (c *Client) Analytics(ctx context.Context, name string, r AnalyticsRequest) AnalyticsResult {
	out := AnalyticsResult{Tool: name, Status: "ok", Request: r, Data: map[string]any{}, Errors: map[string]string{}, Notes: []string{"交易所原始数据与程序派生计算分开标注；不包含模型判断。时间戳除 K 线 time 为秒外，均为 Unix 毫秒；fetchedAt 为 UTC。"}}
	env := "live"
	if c.testnet {
		env = "demo"
	}
	tradingEnvironment := env
	if c.testnet && r.Environment == "" && (name == "get_open_interest" || name == "get_order_flow") {
		r.Environment = "live_reference"
		out.Notes = append(out.Notes, "这类市场统计默认读取币安实盘公开参考数据，因为模拟盘历史统计端点不提供有效数据；source 标注实际来源。交易账户仍为模拟盘，没有切换账户或订单环境。显式 environment=active 可查询模拟盘本身。")
	}
	source := c
	if r.Environment == "live_reference" && name != "get_trading_history" {
		source = newClient("", "", false, c.http)
		env = "live_reference"
	}
	out.Source = AnalyticsSource{Exchange: "Binance USD-M Futures", Environment: env, Host: source.raw.BaseURL, ReferenceOnly: env == "live_reference", TradingEnvironment: tradingEnvironment}
	finish := func() AnalyticsResult {
		out.FetchedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if len(out.Errors) > 0 {
			out.Status = "partial"
			if len(out.Data) == 0 {
				out.Status = "unavailable"
			}
		}
		return out
	}
	if r.Environment != "" && r.Environment != "active" && r.Environment != "live_reference" {
		out.Errors["request"] = "environment 仅支持 active 或 live_reference；模拟盘的持仓量/主动成交统计默认 live_reference，其余默认 active"
		return finish()
	}
	if name == "get_trading_history" && r.Environment == "live_reference" {
		out.Errors["request"] = "成交与收益只能查询当前账户，不能切到实盘参考环境"
		return finish()
	}
	r.Symbol = strings.ToUpper(strings.TrimSpace(r.Symbol))
	if r.Symbol == "" || strings.ContainsAny(r.Symbol, " /?&") {
		out.Errors["request"] = "请填写有效 symbol，例如 BTCUSDT"
		return finish()
	}
	if err := validateAnalytics(name, &r); err != nil {
		out.Errors["request"] = err.Error()
		return finish()
	}
	out.Request = r
	q := url.Values{"symbol": {r.Symbol}, "limit": {strconv.Itoa(r.Limit)}}
	if r.Start > 0 {
		q.Set("startTime", strconv.FormatInt(r.Start, 10))
	}
	if r.End > 0 {
		q.Set("endTime", strconv.FormatInt(r.End, 10))
	}
	section := func(key string, fn func() (any, error)) {
		v, e := fn()
		if e != nil {
			out.Errors[key] = e.Error()
		} else {
			out.Data[key] = v
		}
	}
	switch name {
	case "get_indicators":
		section("indicators", func() (any, error) {
			candles, e := source.KlinesBefore(ctx, r.Symbol, r.Interval, r.Limit, r.End)
			if e != nil {
				return nil, e
			}
			for i, c := range candles {
				if c.Time <= 0 || c.Close <= 0 || c.Low <= 0 || c.High < c.Low || c.Open <= 0 || !finite(c.Open, c.High, c.Low, c.Close, c.Volume) || c.Volume < 0 {
					return nil, fmt.Errorf("K线数据无效")
				}
				if i > 0 && c.Time <= candles[i-1].Time {
					return nil, fmt.Errorf("K线时间顺序无效")
				}
			}
			v := map[string]any{"rows": market.IndicatorSeries(candles), "count": len(candles), "interval": r.Interval, "calculation": "EMA20; MACD=EMA12-EMA26, signal=EMA9(DIF), histogram=DIF-signal（不乘2）; Wilder RSI7 / ATR14"}
			if len(candles) > 0 {
				v["from"] = candles[0].Time * 1000
				v["to"] = candles[len(candles)-1].CloseTime
				v["nextEnd"] = candles[0].Time
			}
			return v, nil
		})
		out.Notes = append(out.Notes, "指标为程序按本页 K线确定性计算。EMA 以首个周期的均值初始化；ATR 首根TR=high-low。null 表示预热不足。不同窗口种子可能导致指标差异，跨页不可当作统一计算序列；扩大窗口可减小差异。closed=false 的值会变化，不代表收盘确认。nextEnd 原值传回 end 查询更早历史，研究数据不更新下单参考价。")
	case "get_funding":
		section("current", func() (any, error) {
			return source.publicObject(ctx, "/fapi/v1/premiumIndex", url.Values{"symbol": {r.Symbol}}, []string{"markPrice", "indexPrice", "lastFundingRate", "nextFundingTime", "time"})
		})
		section("history", func() (any, error) {
			rows, e := source.publicRows(ctx, "/fapi/v1/fundingRate", q, []string{"fundingRate", "fundingTime", "markPrice"})
			if e != nil {
				return nil, e
			}
			return historyPage(rows, "fundingTime", r.Limit), nil
		})
		out.Notes = append(out.Notes, "费率为小数；正费率通常多头付空头，负值反向。current.lastFundingRate 是接口最近费率，不保证下次结算费率；nextFundingTime 为接口给出的下次结算时间。实际已付/收金额见 get_trading_history 的 funding。")
	case "get_open_interest":
		section("current", func() (any, error) {
			return source.publicObject(ctx, "/fapi/v1/openInterest", url.Values{"symbol": {r.Symbol}}, []string{"openInterest", "time"})
		})
		q.Set("period", r.Period)
		section("history", func() (any, error) {
			rows, e := source.publicRows(ctx, "/futures/data/openInterestHist", q, []string{"sumOpenInterest", "sumOpenInterestValue", "timestamp"})
			if e != nil {
				return nil, e
			}
			sortRows(rows, "timestamp")
			for i, row := range rows {
				row["change"] = nil
				row["changePercent"] = nil
				if i > 0 {
					prev, _ := number(rows[i-1]["sumOpenInterest"])
					cur, _ := number(row["sumOpenInterest"])
					row["change"] = cur - prev
					if prev != 0 {
						row["changePercent"] = (cur - prev) / prev * 100
					}
				}
			}
			return historyPage(rows, "timestamp", r.Limit), nil
		})
		out.Notes = append(out.Notes, "持仓量是未平仓合约数量，不能称为净资金流入。sumOpenInterestValue 为接口名义价值；变化率仅相邻返回样本。统计历史由币安提供，通常仅最近一个月。current 与 history 使用同一数据环境，不拼接模拟盘当前值与实盘历史。显式 active 查询失败时可用 live_reference 获取公开参考，但不切换交易账户。")
	case "get_order_flow":
		q.Set("period", r.Period)
		section("flow", func() (any, error) {
			rows, e := source.publicRows(ctx, "/futures/data/takerlongshortRatio", q, []string{"buyVol", "sellVol", "buySellRatio", "timestamp"})
			if e != nil {
				return nil, e
			}
			sortRows(rows, "timestamp")
			cvd := 0.0
			for _, row := range rows {
				buy, _ := number(row["buyVol"])
				sell, _ := number(row["sellVol"])
				if buy < 0 || sell < 0 {
					return nil, fmt.Errorf("成交量无效")
				}
				delta := buy - sell
				cvd += delta
				row["delta"] = delta
				row["windowCVD"] = cvd
				row["imbalance"] = nil
				if buy+sell > 0 {
					row["imbalance"] = delta / (buy + sell)
				}
			}
			return historyPage(rows, "timestamp", r.Limit), nil
		})
		out.Notes = append(out.Notes, "buyVol/sellVol 为主动买/卖成交量，单位是基础资产；delta=buyVol-sellVol；imbalance=delta/(buyVol+sellVol)。windowCVD 仅本次窗口累积差值，不是全市场历史CVD。币安此统计仅提供最近30天，不能当成逐笔实时数据。")
	case "get_order_book":
		section("book", func() (any, error) { return source.orderBook(ctx, r.Symbol, r.Limit, r.NotionalUSD) })
		out.Notes = append(out.Notes, "盘口为采集时快照，非持续订阅。只含返回档位，不含 RPI 或隐藏流动性。滑点按 notionalUSD/mid 的固定基础币数量逐档计算；不含手续费、延迟和盘口后续变化。深度不足时 fullOrderVWAP/slippageBps=null，禁止当作可完整成交报价。")
	case "get_trading_history":
		now := time.Now()
		to := r.End
		if to == 0 {
			to = now.UnixMilli()
		}
		from := r.Start
		if from == 0 {
			switch r.Period {
			case "day":
				local := time.UnixMilli(to).In(time.FixedZone("UTC+8", 8*3600))
				from = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()).UnixMilli()
			case "30d":
				from = to - int64(30*24*time.Hour/time.Millisecond)
			default:
				from = to - int64(7*24*time.Hour/time.Millisecond)
			}
		}
		if from > to || to-from > int64(90*24*time.Hour/time.Millisecond) {
			out.Errors["request"] = "收益窗口需要 start<=end，且不超过90天（交易所保留期）"
			break
		}
		section("income", func() (any, error) {
			s, e := c.incomeSummary(ctx, r.Symbol, from, to)
			if e != nil {
				return nil, e
			}
			return map[string]any{"summary": s, "complete": true, "symbol": r.Symbol, "from": from, "to": to, "asset": "USDT", "scope": "完整分页收入流水；仅USDT已实现盈亏、手续费及资金费，不含转账/充值和非USDT费用"}, nil
		})
		fillFrom := from
		if to-fillFrom > int64(7*24*time.Hour/time.Millisecond) {
			fillFrom = to - int64(7*24*time.Hour/time.Millisecond)
		}
		section("fills", func() (any, error) {
			b, e := c.signedGet(ctx, "/fapi/v1/userTrades", url.Values{"symbol": {r.Symbol}, "startTime": {strconv.FormatInt(fillFrom, 10)}, "endTime": {strconv.FormatInt(to, 10)}, "limit": {strconv.Itoa(r.Limit)}})
			if e != nil {
				return nil, e
			}
			rows, e := decodeRows(b, []string{"id", "orderId", "price", "qty", "realizedPnl", "commission", "time"})
			if e != nil {
				return nil, e
			}
			for _, row := range rows {
				if row["commissionAsset"] == nil || row["side"] == nil || row["positionSide"] == nil {
					return nil, fmt.Errorf("成交字段缺失")
				}
			}
			sortRows(rows, "time")
			capped := len(rows) >= r.Limit
			if capped {
				out.Errors["fillsCoverage"] = "成交返回数量达到limit，可能截断；缩小 start/end 时间窗口继续查询，不能据此估计全期胜率"
			}
			return map[string]any{"rows": rows, "count": len(rows), "from": fillFrom, "to": to, "possiblyTruncated": capped, "completeForIncomeWindow": !capped && fillFrom == from, "scope": "单次成交样本，最长7天；不等于闭合交易集合"}, nil
		})
		out.Notes = append(out.Notes, "收益汇总与成交样本具有独立覆盖范围；30d 收益仍完整分页，但成交最多末7天，更多成交请按7天或更小窗口读取。手续费保留 commissionAsset，不混加不同币种。realizedPnl=0 不代表一定是开仓。不从成交样本推断全期胜率或账户回撤。")
	}
	return finish()
}

func validateAnalytics(name string, r *AnalyticsRequest) error {
	if !IsAnalyticsTool(name) {
		return fmt.Errorf("未知数据工具")
	}
	if r.Start < 0 || r.End < 0 || !finite(r.NotionalUSD) || r.NotionalUSD < 0 {
		return fmt.Errorf("时间和名义金额必须非负且有限")
	}
	if name == "get_indicators" {
		if r.Interval == "" {
			r.Interval = "15m"
		}
		if !ValidInterval(r.Interval) {
			return fmt.Errorf("K线周期无效")
		}
		if r.Limit == 0 {
			r.Limit = 200
		}
		if r.Limit < 1 || r.Limit > 1500 {
			return fmt.Errorf("每次支持1–1500根K线，可使用nextEnd分页")
		}
		if r.Start != 0 {
			return fmt.Errorf("指标历史请使用end向前分页")
		}
		return nil
	}
	if r.Start > 0 && r.Start < 1e12 {
		r.Start *= 1000
	}
	if r.End > 0 && r.End < 1e12 {
		r.End *= 1000
	}
	if r.Start > 0 && r.End > 0 && r.Start > r.End {
		return fmt.Errorf("start 不能晚于 end")
	}
	max := 1000
	switch name {
	case "get_funding":
		if r.Limit == 0 {
			r.Limit = 30
		}
	case "get_open_interest", "get_order_flow":
		max = 500
		if r.Limit == 0 {
			r.Limit = 30
		}
		if r.Period == "" {
			r.Period = "5m"
		}
		switch r.Period {
		case "5m", "15m", "30m", "1h", "2h", "4h", "6h", "12h", "1d":
		default:
			return fmt.Errorf("统计周期支持5m/15m/30m/1h/2h/4h/6h/12h/1d")
		}
	case "get_order_book":
		if r.Limit == 0 {
			r.Limit = 20
		}
		switch r.Limit {
		case 5, 10, 20, 50, 100, 500, 1000:
		default:
			return fmt.Errorf("盘口档位仅支持5/10/20/50/100/500/1000")
		}
	case "get_trading_history":
		if r.Limit == 0 {
			r.Limit = 100
		}
		if r.Period == "" {
			r.Period = "7d"
		}
		if r.Period != "day" && r.Period != "7d" && r.Period != "30d" {
			return fmt.Errorf("收益 period 支持 day（UTC+8）、7d、30d")
		}
	}
	if r.Limit < 1 || r.Limit > max {
		return fmt.Errorf("limit 需要在1–%d之间", max)
	}
	return nil
}

func (c *Client) publicGet(ctx context.Context, path string, q url.Values) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, c.raw.BaseURL+path+"?"+q.Encode(), nil)
	if e != nil {
		return nil, e
	}
	res, e := c.http.Do(req)
	if e != nil {
		return nil, fmt.Errorf("公开数据请求失败: %w", e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 2<<20 {
		return nil, fmt.Errorf("数据响应超过2MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s HTTP %d", path, res.StatusCode)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("%s 未返回有效JSON（该环境可能不支持此端点）", path)
	}
	return b, nil
}
func number(v any) (float64, error) {
	var x float64
	var e error
	switch v := v.(type) {
	case string:
		x, e = strconv.ParseFloat(v, 64)
	case json.Number:
		x, e = v.Float64()
	case float64:
		x = v
	default:
		return 0, fmt.Errorf("数字字段缺失或类型无效")
	}
	if e != nil || !finite(x) {
		return 0, fmt.Errorf("数字字段无效")
	}
	return x, nil
}
func finite(xs ...float64) bool {
	for _, x := range xs {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}
func decodeJSON(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e := d.Decode(out); e != nil {
		return fmt.Errorf("响应格式无效: %w", e)
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return fmt.Errorf("响应包含额外内容")
	}
	return nil
}
func validateRow(row map[string]any, required []string) error {
	if row == nil {
		return fmt.Errorf("响应为空对象")
	}
	if code, ok := row["code"]; ok {
		n, e := number(code)
		if e != nil || n != 0 {
			return fmt.Errorf("交易所返回错误码 %v: %v", code, row["msg"])
		}
	}
	for _, k := range required {
		if _, e := number(row[k]); e != nil {
			return fmt.Errorf("%s: %w", k, e)
		}
	}
	return nil
}
func decodeRows(b []byte, required []string) ([]map[string]any, error) {
	var rows []map[string]any
	if e := decodeJSON(b, &rows); e != nil {
		return nil, e
	}
	if rows == nil {
		return nil, fmt.Errorf("响应不是数据数组")
	}
	for _, r := range rows {
		if e := validateRow(r, required); e != nil {
			return nil, e
		}
	}
	return rows, nil
}
func (c *Client) publicObject(ctx context.Context, path string, q url.Values, fields []string) (map[string]any, error) {
	b, e := c.publicGet(ctx, path, q)
	if e != nil {
		return nil, e
	}
	var row map[string]any
	if e = decodeJSON(b, &row); e != nil {
		return nil, e
	}
	if e = validateRow(row, fields); e != nil {
		return nil, e
	}
	return row, nil
}
func (c *Client) publicRows(ctx context.Context, path string, q url.Values, fields []string) ([]map[string]any, error) {
	b, e := c.publicGet(ctx, path, q)
	if e != nil {
		return nil, e
	}
	return decodeRows(b, fields)
}
func sortRows(rows []map[string]any, key string) {
	sort.SliceStable(rows, func(i, j int) bool { a, _ := number(rows[i][key]); b, _ := number(rows[j][key]); return a < b })
}
func historyPage(rows []map[string]any, key string, limit int) map[string]any {
	sortRows(rows, key)
	m := map[string]any{"rows": rows, "count": len(rows), "atLimit": len(rows) >= limit}
	if len(rows) > 0 {
		a, _ := number(rows[0][key])
		b, _ := number(rows[len(rows)-1][key])
		m["from"] = int64(a)
		m["to"] = int64(b)
		m["nextStart"] = int64(b) + 1
		m["previousEnd"] = int64(a) - 1
	}
	return m
}
