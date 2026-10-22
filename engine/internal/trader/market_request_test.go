package trader

import (
	"context"
	"desk/internal/agenttools"
	"desk/internal/model"
	"desk/internal/prompts"
	"strings"
	"testing"
)

type capturedMarket struct {
	symbol, interval string
	limit            int
	end              int64
	calls            int
}

func (m *capturedMarket) KlinesBefore(_ context.Context, symbol, interval string, limit int, end int64) ([]model.Candle, error) {
	m.symbol, m.interval, m.limit, m.end = symbol, interval, limit, end
	m.calls++
	return []model.Candle{{Time: 1700000000, Close: 100}, {Time: 1700003600, Close: 101}}, nil
}

func TestAgentChoosesMarketOutsideTradingAllowlist(t *testing.T) {
	source := &capturedMarket{}
	a := &agentState{marketSource: source, strategy: model.Strategy{Coins: []string{"BTCUSDT"}, Timeframe: "5m", KlineCount: 30}}
	text := a.market(context.Background(), `{"symbol":" solusdt ","interval":"1w","limit":1500,"end":1700100000000}`)
	if source.symbol != "SOLUSDT" || source.interval != "1w" || source.limit != 1500 || source.end != 1700100000000 {
		t.Fatalf("agent choices overridden: %+v", source)
	}
	if !strings.Contains(text, "nextEnd=1700000000") || !strings.Contains(text, "SOLUSDT 1w") {
		t.Fatal("missing pagination/interval", text)
	}
	if _, ok := a.marks["SOLUSDT"]; ok {
		t.Fatal("historical price became current order price")
	}
	plan := Apply(a.strategy, 1000, 0, nil, 101, model.Decision{Symbol: "SOLUSDT", Action: "open_long", PositionSizeUSD: 100, Leverage: 1})
	if !strings.Contains(plan.Skip, "不在策略币种里") {
		t.Fatal("reading outside the list enabled trading")
	}
	a.market(context.Background(), `{"symbol":"SOLUSDT","interval":"3m","limit":200}`)
	if a.marks["SOLUSDT"] != 101 {
		t.Fatal("latest market price not available")
	}
}

func TestInvalidMarketRequestsDoNotCallExchange(t *testing.T) {
	source := &capturedMarket{}
	a := &agentState{marketSource: source}
	for _, args := range []string{`{`, `{"interval":"nonsense"}`, `{"limit":1501}`, `{"limit":0}`, `{"end":-1}`, `{"start":"2026-09-01"}`, `{"timeframe":"1d"}`, `{"end":"yesterday"}`, `{} {}`, `null`} {
		if a.market(context.Background(), args) == "" {
			t.Fatal("validation message missing")
		}
	}
	if source.calls != 0 {
		t.Fatal("invalid request reached exchange")
	}
}

func TestPromptsAreUsedInCloudRequest(t *testing.T) {
	c := prompts.Defaults()
	c["system"] = "trade cap {{positionValueRatio}}; custom strategy; custom history"
	toolsConfig := agenttools.Config{Instructions: "custom tools for {{coins}}"}
	st := model.Strategy{PositionValueRatio: 2, Coins: []string{"BTCUSDT"}}
	got := systemPrompt(st, c, toolsConfig, "workspace rules {{coins}}")
	for _, want := range []string{"trade cap 2.00", "custom tools", "custom strategy", "custom history"} {
		if !strings.Contains(got, want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Count(got, "custom tools") != 1 || !strings.Contains(got, "custom tools for BTCUSDT") {
		t.Fatal("independent tools not rendered exactly once")
	}
	if strings.Contains(prompts.Instructions(c, model.Strategy{}), "custom tools") {
		t.Fatal("tool instructions leaked into prompt-only preview")
	}
	c["chat"] = "chat scenario"
	chat := chatPrompt(model.Strategy{}, c, toolsConfig, nil)
	if strings.Contains(chat, "custom tools") || !strings.Contains(chat, "chat scenario") {
		t.Fatal("chat duplicated system context or missed its scenario")
	}
	body := cloudPrompt(st, c, toolsConfig, []memory{{Role: "用户", Text: "real message"}})
	if !strings.Contains(body, "real message") || strings.Contains(body, "custom tools") || strings.Contains(body, "trade cap") {
		t.Fatal("request body contains static system context")
	}
}
