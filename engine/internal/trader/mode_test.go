package trader

import (
	"context"
	"desk/internal/admin"
	"desk/internal/ai"
	"desk/internal/model"
	"desk/internal/opencode"
	"desk/internal/store"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSwitchModeCancelsRunningLoopAndPreservesContext(t *testing.T) {
	s := batchService(t)
	s.say("user", "", "trade-only-context")
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	s.loopActive.Store(true)
	s.store.SetRunning(true)
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		s.say("agent", "", "old worker final message")
		s.loopActive.Store(false)
		close(done)
	}()
	state, e := s.SwitchMode(context.Background(), "optimization", "trading")
	if e != nil {
		t.Fatal(e)
	}
	<-done
	if state.Running || state.Busy || state.Mode != "optimization" || len(s.Log()) != 0 {
		t.Fatal(state, s.Log())
	}
	s.say("user", "", "research-only-context")
	state, e = s.SwitchMode(context.Background(), "trading", "optimization")
	if e != nil {
		t.Fatal(e)
	}
	log := s.Log()
	if len(log) < 2 || log[0].Text != "trade-only-context" || strings.Contains(s.transcriptFromLog()[0].Text, "research-only") {
		t.Fatal(log)
	}
	if state.Running || state.Busy {
		t.Fatal("switch resumed trading")
	}
	if e = s.AskConversation("stale request", "optimization"); e != store.ErrConversationChanged {
		t.Fatal(e)
	}
	if e = s.StartConversation("optimization"); e != store.ErrConversationChanged {
		t.Fatal(e)
	}
}
func TestOptimizationBlocksAccountOrdersAndTradingFileChanges(t *testing.T) {
	s := batchService(t)
	s.SwitchMode(context.Background(), "optimization", "trading")
	exchange := submitFixture()
	a := &agentState{service: s, client: exchange, strategy: submitStrategy(), marks: map[string]float64{"BTCUSDT": 100}}
	for _, name := range []string{"get_account", "get_positions", "get_market", "get_trading_history", "submit_decisions", "set_protection"} {
		result := a.call(context.Background(), ai.ToolCall{Name: name, Arguments: `{}`})
		if !strings.Contains(result, "策略优化模式") {
			t.Fatal(name, result)
		}
	}
	reads := a.readTool(context.Background(), toolCall{Name: "get_account", Args: `{}`})
	if !strings.Contains(reads.text, "禁止") {
		t.Fatal(reads)
	}
	result := a.call(context.Background(), ai.ToolCall{Name: "write_file", Arguments: `{"path":"trading.md","revision":0,"content":"overwrite live"}`})
	if !strings.Contains(result, "仅可写入") {
		t.Fatal(result)
	}
	for _, call := range []ai.ToolCall{
		{Name: "create_directory", Arguments: `{"path":"优化"}`},
		{Name: "write_file", Arguments: `{"path":"优化/v1.md","revision":0,"content":"nested"}`},
		{Name: "write_file", Arguments: `{"path":"策略.md","revision":0,"content":"duplicate"}`},
	} {
		if denial := a.modeDenial(call.Name, call.Arguments); denial == "" {
			t.Fatal("unexpected write permission", call)
		}
	}
	result = a.call(context.Background(), ai.ToolCall{Name: "write_file", Arguments: `{"path":"复盘.md","revision":0,"content":"问题、证据、修正原因"}`})
	if !strings.Contains(result, "revision") {
		t.Fatal(result)
	}
	var called atomic.Bool
	if e := s.TradingAction(func() { called.Store(true) }); e == nil || called.Load() {
		t.Fatal("manual trade allowed")
	}
	f, err := s.modeInvest()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.store.Prompts()
	cfg["trading"] = "LIVE-INSTRUCTIONS"
	cfg["optimization"] = "RESEARCH-INSTRUCTIONS"
	tools, _ := s.store.Tools()
	system := s.modeSystemPrompt(model.Strategy{}, cfg, tools, f.Content)
	if strings.Contains(system, "LIVE-INSTRUCTIONS") || !strings.Contains(system, "RESEARCH-INSTRUCTIONS") {
		t.Fatal(system)
	}
}

type blockingModeRemote struct {
	entered  chan struct{}
	prepared []opencode.Input
}

func (r *blockingModeRemote) Prepare(_ context.Context, in opencode.Input) error {
	r.prepared = append(r.prepared, in)
	return nil
}
func (r *blockingModeRemote) Say(ctx context.Context, _, _ string) (string, error) {
	close(r.entered)
	<-ctx.Done()
	return "old-model-output", ctx.Err()
}
func TestModeSwitchCancelsActivePanelBeforeNewConversation(t *testing.T) {
	s := batchService(t)
	s.store.SaveModel("test", "test", "test", "test")
	r := &blockingModeRemote{entered: make(chan struct{})}
	s.remote = r
	s.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		return admin.Resolved{Model: "fake"}, nil
	}
	if e := s.Ask("old panel question"); e != nil {
		t.Fatal(e)
	}
	select {
	case <-r.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("reply did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	state, e := s.SwitchMode(ctx, "optimization", "trading")
	if e != nil || state.Busy || len(s.Log()) != 0 {
		t.Fatal(state, e, s.Log())
	}
	if len(r.prepared) != 1 || r.prepared[0].ConversationID != "trading" {
		t.Fatal(r.prepared)
	}
}

type optimizationLoopRemote struct {
	cancel  context.CancelFunc
	calls   int
	input   opencode.Input
	prompts []string
}

func (r *optimizationLoopRemote) Prepare(_ context.Context, in opencode.Input) error {
	r.input = in
	return nil
}
func (r *optimizationLoopRemote) Say(_ context.Context, _, prompt string) (string, error) {
	r.calls++
	r.prompts = append(r.prompts, prompt)
	if r.calls == 1 {
		return "```tool\n{\"name\":\"get_account\",\"arguments\":{}}\n```\n```tool\n{\"name\":\"list_files\",\"arguments\":{\"path\":\"\"}}\n```", nil
	}
	r.cancel()
	return "研究结束", nil
}
func TestOptimizationLoopRunsWithoutTradingClientOrCoins(t *testing.T) {
	s := batchService(t)
	if _, e := s.SwitchMode(context.Background(), "optimization", "trading"); e != nil {
		t.Fatal(e)
	}
	strategy, _ := s.store.Strategy()
	strategy.Coins = nil
	s.store.SaveStrategy(strategy)
	s.store.SaveModel("test", "test", "test", "test")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	remote := &optimizationLoopRemote{cancel: cancel}
	s.remote = remote
	s.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		return admin.Resolved{Model: "fake"}, nil
	}
	s.newClient = func(string, string, bool) agentClient { t.Error("optimization created a trading client"); return nil }
	if e := s.cloudLoop(ctx); e != nil {
		t.Fatal(e)
	}
	if remote.calls != 2 || remote.input.ConversationID != "optimization" || !strings.Contains(remote.input.SystemPrompt, "策略研究与优化代理") || !strings.Contains(remote.prompts[1], "tool_unavailable") {
		t.Fatal(remote)
	}
}
