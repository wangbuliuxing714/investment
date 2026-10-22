package trader

import (
	"context"
	"desk/internal/admin"
	"desk/internal/ai"
	"desk/internal/history"
	"desk/internal/opencode"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalHistoryNeverChangesExecutablePrices(t *testing.T) {
	svc := batchService(t)
	h, e := history.Open(filepath.Join(t.TempDir(), "market.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	svc.AttachHistory(h)
	a := &agentState{service: svc, marks: map[string]float64{"BTCUSDT": 123}}
	result := a.call(context.Background(), ai.ToolCall{Name: "get_history", Arguments: `{"market":"spot","interval":"1w"}`})
	if !strings.Contains(result, `"status":"pending"`) || !strings.Contains(result, `"candles":[]`) || a.marks["BTCUSDT"] != 123 {
		t.Fatal(result, a.marks)
	}
	result = a.localHistory(context.Background(), `{"market":"bad"}`)
	if strings.Contains(result, `"candles"`) {
		t.Fatal("bad market accepted")
	}
}

type historyRemote struct{ prompts []string }

func (r *historyRemote) Prepare(context.Context, opencode.Input) error { return nil }
func (r *historyRemote) Say(_ context.Context, _, prompt string) (string, error) {
	r.prompts = append(r.prompts, prompt)
	if len(r.prompts) == 1 {
		return "```tool\n{\"name\":\"get_history\",\"arguments\":{\"market\":\"um\"}}\n```", nil
	}
	return "历史尚待下载。", nil
}
func TestPanelHistoryContinuesWithoutExchangeCredentials(t *testing.T) {
	svc := batchService(t)
	svc.store.SaveModel("test", "test", "test", "test")
	svc.store.SaveBinance("", "", false)
	h, e := history.Open(filepath.Join(t.TempDir(), "market.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	svc.AttachHistory(h)
	remote := &historyRemote{}
	svc.remote = remote
	svc.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		return admin.Resolved{Model: "fake"}, nil
	}
	if e = svc.answer(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(remote.prompts) != 2 || !strings.Contains(remote.prompts[1], `"market":"um"`) || !strings.Contains(remote.prompts[1], `"status":"pending"`) {
		t.Fatal("local result did not reach model", remote.prompts)
	}
}
