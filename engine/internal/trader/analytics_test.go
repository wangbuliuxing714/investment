package trader

import (
	"context"
	"desk/internal/agenttools"
	"desk/internal/binance"
	"encoding/json"
	"strings"
	"testing"
)

type researchExchange struct {
	*submitExchange
	read func(context.Context, string) binance.AnalyticsResult
}

func (f *researchExchange) Analytics(ctx context.Context, name string, r binance.AnalyticsRequest) binance.AnalyticsResult {
	return f.read(ctx, name)
}
func TestAllAnalyticsToolsParallelAndReadOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 6)
	release := make(chan struct{})
	f := &researchExchange{submitExchange: submitFixture(), read: func(ctx context.Context, name string) binance.AnalyticsResult {
		started <- name
		select {
		case <-release:
		case <-ctx.Done():
		}
		return binance.AnalyticsResult{Tool: name, Status: "ok", Data: map[string]any{"symbol": "BTCUSDT"}}
	}}
	a := &agentState{client: f, marks: map[string]float64{"BTCUSDT": 100}}
	var calls []toolCall
	for _, d := range agenttools.Definitions {
		if binance.IsAnalyticsTool(d.Name) {
			if !knownTool(d.Name) || !readOnlyTool(d.Name) || !strings.Contains(agenttools.DefaultInstructions, d.Name) {
				t.Fatal("tool not fully wired", d.Name)
			}
			calls = append(calls, toolCall{Name: d.Name, Args: `{"symbol":"BTCUSDT"}`})
		}
	}
	if len(calls) != 6 {
		t.Fatal(len(calls))
	}
	done := make(chan []memory, 1)
	go func() { done <- a.readBatch(ctx, calls, func(toolCall) {}, func(toolCall, string) {}) }()
	for range calls {
		receive(t, started)
	}
	close(release)
	mem := receive(t, done)
	if len(mem) != 6 || len(a.marks) != 1 || a.marks["BTCUSDT"] != 100 {
		t.Fatal("research altered executable prices")
	}
	for _, m := range mem {
		if !json.Valid([]byte(m.Text)) {
			t.Fatal(m.Text)
		}
	}
	for _, name := range []string{"submit_decisions", "set_protection", "wait"} {
		if readOnlyTool(name) {
			t.Fatal("write/barrier became parallel", name)
		}
	}
}
func TestAnalyticsRejectsInvalidArgumentsWithoutCallingExchange(t *testing.T) {
	f := &researchExchange{submitExchange: submitFixture(), read: func(context.Context, string) binance.AnalyticsResult {
		t.Fatal("invalid args dispatched")
		return binance.AnalyticsResult{}
	}}
	a := &agentState{client: f}
	for _, raw := range []string{"null", "[]", `{"foo":1}`, `{"symbol":"BTCUSDT"}{}`} {
		v := a.analytics(context.Background(), toolCall{Name: "get_funding", Args: raw})
		if !json.Valid([]byte(v)) || !strings.Contains(v, "unavailable") {
			t.Fatal(v)
		}
	}
}
func TestAnalyticsResultsPersistWholeBeyondOldClip(t *testing.T) {
	s := batchService(t)
	text := `{"rows":"` + strings.Repeat("1234567890", 60000) + `","tail":"latest"}`
	s.say("result", "get_indicators", text)
	logs := s.Log()
	if logs[len(logs)-1].Text != text {
		t.Fatal("structured result was truncated")
	}
}
