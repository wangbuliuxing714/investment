package trader

import (
	"context"
	"desk/internal/agenttools"
	"desk/internal/binance"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type analyticsReader interface {
	Analytics(context.Context, string, binance.AnalyticsRequest) binance.AnalyticsResult
}

func (a *agentState) analytics(ctx context.Context, call toolCall) string {
	var r binance.AnalyticsRequest
	fail := func(err error) string {
		b, _ := json.Marshal(map[string]any{"tool": call.Name, "status": "unavailable", "data": map[string]any{}, "errors": map[string]string{"request": err.Error()}})
		return string(b)
	}
	d := json.NewDecoder(strings.NewReader(call.Args))
	d.DisallowUnknownFields()
	if e := d.Decode(&r); e != nil {
		return fail(e)
	}
	if e := d.Decode(new(any)); e != io.EOF || strings.TrimSpace(call.Args) == "null" {
		return fail(fmt.Errorf("参数必须是单个JSON对象"))
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(call.Args), &fields)
	allowed := map[string]bool{}
	for _, definition := range agenttools.Definitions {
		if definition.Name == call.Name {
			for _, p := range definition.Parameters {
				allowed[p.Name] = true
			}
		}
	}
	for field := range fields {
		if !allowed[field] {
			return fail(fmt.Errorf("%s 不支持参数 %s", call.Name, field))
		}
	}
	source, ok := a.client.(analyticsReader)
	if !ok {
		return fail(fmt.Errorf("当前数据源不支持此只读工具"))
	}
	result := source.Analytics(ctx, call.Name, r)
	if a.paper && call.Name == "get_trading_history" {
		result.Notes = append(result.Notes, "当前采用本地模拟执行；这些是绑定交易所账户的流水，不包含本地模拟订单。")
	}
	b, e := json.Marshal(result)
	if e != nil {
		return fail(e)
	}
	return string(b)
}
