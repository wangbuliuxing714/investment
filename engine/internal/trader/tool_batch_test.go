package trader

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"desk/internal/admin"
	"desk/internal/binance"
	"desk/internal/model"
	"desk/internal/opencode"
	"desk/internal/store"
)

// All exchange and model operations below are in-memory; no orders or network.
type batchExchange struct {
	*submitExchange
	read  func(context.Context, string) error
	order func() error
}

func (f *batchExchange) Account(ctx context.Context) (model.Account, error) {
	if err := f.read(ctx, "account"); err != nil {
		return model.Account{}, err
	}
	return f.account, nil
}
func (f *batchExchange) Positions(ctx context.Context) ([]model.Position, error) {
	if err := f.read(ctx, "positions"); err != nil {
		return nil, err
	}
	return append([]model.Position(nil), f.positions...), nil
}
func (f *batchExchange) KlinesBefore(ctx context.Context, symbol, interval string, limit int, end int64) ([]model.Candle, error) {
	if err := f.read(ctx, symbol); err != nil {
		return nil, err
	}
	return []model.Candle{{Time: 1790751600, Open: f.price, High: f.price, Low: f.price, Close: f.price}}, nil
}
func (f *batchExchange) Market(ctx context.Context, order binance.MarketOrder) (binance.Fill, error) {
	if f.order != nil {
		if err := f.order(); err != nil {
			return binance.Fill{}, err
		}
	}
	return f.submitExchange.Market(ctx, order)
}

func batchService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, nil)
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent operation did not complete")
		var zero T
		return zero
	}
}

func marketCall(symbol string) toolCall {
	return toolCall{Name: "get_market", Args: fmt.Sprintf("{\"symbol\":%q,\"interval\":\"1h\",\"limit\":1}", symbol)}
}

func TestReadBatchRunsIndependentToolsConcurrently(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 4)
	release := make(chan struct{})
	f := &batchExchange{submitExchange: submitFixture(), read: func(ctx context.Context, key string) error {
		started <- key
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	a := &agentState{client: f, strategy: submitStrategy()}
	calls := []toolCall{{Name: "get_account", Args: "{}"}, {Name: "get_positions", Args: "{}"}, marketCall("BTCUSDT"), marketCall("ETHUSDT")}
	done := make(chan []memory, 1)
	go func() { done <- a.readBatch(ctx, calls, func(toolCall) {}, func(toolCall, string) {}) }()
	seen := map[string]bool{}
	for range calls {
		seen[receive(t, started)] = true
	}
	if len(seen) != 4 {
		t.Fatalf("duplicate account/position requests: %v", seen)
	}
	close(release)
	results := receive(t, done)
	if len(results) != 4 || a.account.Equity != 1000 || a.marks["BTCUSDT"] != 100 || a.marks["ETHUSDT"] != 100 {
		t.Fatalf("snapshots missing: %+v", a)
	}
}

func TestReadBatchReversedRepliesKeepSameNameCardsPaired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	releaseBTC := make(chan struct{})
	ethReady := make(chan struct{})
	f := &batchExchange{submitExchange: submitFixture(), read: func(ctx context.Context, key string) error {
		if key == "BTCUSDT" {
			select {
			case <-releaseBTC:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if key == "ETHUSDT" {
			close(ethReady)
		}
		return nil
	}}
	a := &agentState{client: f, strategy: submitStrategy()}
	calls := []toolCall{marketCall("BTCUSDT"), marketCall("ETHUSDT"), {Name: "get_account", Args: "{}"}}
	finished := make(chan string, 3)
	done := make(chan []memory, 1)
	go func() {
		done <- a.readBatch(ctx, calls, func(toolCall) {}, func(call toolCall, text string) { finished <- call.Name + ":" + call.Args })
	}()
	receive(t, ethReady)
	if got := receive(t, finished); !strings.HasPrefix(got, "get_account:") {
		t.Fatal("same-name results reordered", got)
	}
	close(releaseBTC)
	if got := receive(t, finished); !strings.Contains(got, "BTCUSDT") {
		t.Fatal(got)
	}
	if got := receive(t, finished); !strings.Contains(got, "ETHUSDT") {
		t.Fatal(got)
	}
	results := receive(t, done)
	if !strings.Contains(results[0].Text, "BTCUSDT") || !strings.Contains(results[1].Text, "ETHUSDT") {
		t.Fatal("model results reordered")
	}
}

func TestReadBatchBoundsConcurrencyWithoutDroppingCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 24)
	release := make(chan struct{})
	var active, peak, total atomic.Int32
	f := &batchExchange{submitExchange: submitFixture(), read: func(ctx context.Context, key string) error {
		n := active.Add(1)
		defer active.Add(-1)
		total.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	a := &agentState{client: f}
	calls := make([]toolCall, 24)
	for i := range calls {
		calls[i] = marketCall(fmt.Sprintf("COIN%dUSDT", i))
	}
	done := make(chan []memory, 1)
	go func() { done <- a.readBatch(ctx, calls, func(toolCall) {}, func(toolCall, string) {}) }()
	for i := 0; i < maxParallelReads; i++ {
		receive(t, started)
	}
	if active.Load() != maxParallelReads {
		t.Fatal("reads were serialized", active.Load())
	}
	close(release)
	results := receive(t, done)
	if len(results) != 24 || total.Load() != 24 || peak.Load() > maxParallelReads {
		t.Fatal(len(results), total.Load(), peak.Load())
	}
	prompt := transcriptText([]memory{toolBatchMemory(results)}, "history")
	for i := range calls {
		if !strings.Contains(prompt, fmt.Sprintf("COIN%dUSDT", i)) {
			t.Fatalf("parallel result %d lost from model context", i)
		}
	}
}

func TestReadFailureAndPanicDoNotDiscardSiblingResults(t *testing.T) {
	for _, panicRead := range []bool{false, true} {
		t.Run(fmt.Sprint(panicRead), func(t *testing.T) {
			f := &batchExchange{submitExchange: submitFixture(), read: func(ctx context.Context, key string) error {
				if key == "ETHUSDT" {
					if panicRead {
						panic("read failed")
					}
					return errors.New("read failed")
				}
				return nil
			}}
			a := &agentState{client: f, marks: map[string]float64{"ETHUSDT": 99}}
			results := a.readBatch(context.Background(), []toolCall{marketCall("ETHUSDT"), marketCall("BTCUSDT"), {Name: "get_account", Args: "{}"}}, func(toolCall) {}, func(toolCall, string) {})
			if !strings.Contains(results[0].Text, "read failed") || a.marks["ETHUSDT"] != 99 || a.marks["BTCUSDT"] != 100 || a.account.Equity != 1000 {
				t.Fatal("failed read corrupted successful snapshots")
			}
		})
	}
}

func TestOrderBarriersPreservePreAndPostTradeSnapshots(t *testing.T) {
	svc := batchService(t)
	var active, preReads atomic.Int32
	f := &batchExchange{submitExchange: submitFixture(), read: func(ctx context.Context, key string) error {
		active.Add(1)
		defer active.Add(-1)
		if key == "PRE1" || key == "PRE2" {
			preReads.Add(1)
		}
		return nil
	}, order: func() error {
		if active.Load() != 0 || preReads.Load() != 2 {
			return errors.New("order crossed read barrier")
		}
		return nil
	}}
	a := &agentState{client: f, strategy: submitStrategy()}
	open := toolCall{Name: "submit_decisions", Args: "{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"open_long\",\"position_size_usd\":100,\"leverage\":1}]}"}
	closeCall := toolCall{Name: "submit_decisions", Args: "{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"close_long\"}]}"}
	calls := []toolCall{marketCall("PRE1"), marketCall("PRE2"), open, {Name: "get_positions", Args: "{}"}, closeCall, {Name: "get_positions", Args: "{}"}}
	mem, waited, _ := svc.runToolCalls(context.Background(), a, calls, true)
	if waited != 0 || len(f.orders) != 2 || !strings.Contains(mem[3].Text, "BTCUSDT long") || mem[5].Text != "无持仓" {
		t.Fatalf("barriers or snapshots wrong: orders=%d mem=%+v", len(f.orders), mem)
	}
	if f.orders[0].Reduce || !f.orders[1].Reduce {
		t.Fatal("order sequence changed")
	}
	lines := svc.Log()
	if len(lines) != 12 {
		t.Fatal("tool cards/results missing", len(lines))
	}
}

func TestCancelBatchDoesNotStartOrdersOrLaterReads(t *testing.T) {
	svc := batchService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	f := &batchExchange{submitExchange: submitFixture(), read: func(ctx context.Context, key string) error { started <- struct{}{}; <-ctx.Done(); return ctx.Err() }}
	a := &agentState{client: f, strategy: submitStrategy()}
	calls := []toolCall{{Name: "get_account", Args: "{}"}, marketCall("BTCUSDT"), {Name: "submit_decisions", Args: "{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"open_long\"}]}"}, marketCall("ETHUSDT")}
	done := make(chan struct{}, 1)
	go func() { svc.runToolCalls(ctx, a, calls, true); done <- struct{}{} }()
	receive(t, started)
	cancel()
	receive(t, done)
	if len(f.orders) != 0 || len(svc.Log()) != 4 {
		t.Fatal("cancel started another tool or lost result events")
	}
}

type scriptedRemote struct {
	replies int
	cancel  context.CancelFunc
	prompts []string
}

func (r *scriptedRemote) Prepare(context.Context, opencode.Input) error { return nil }
func (r *scriptedRemote) Say(ctx context.Context, modelName, prompt string) (string, error) {
	r.prompts = append(r.prompts, prompt)
	r.replies++
	if r.replies > 10 {
		r.cancel()
		return "", context.Canceled
	}

	return "```tool\n{\"name\":\"submit_decisions\",\"arguments\":{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"hold\"}]}}\n```", nil
}

func TestCloudLoopHasNoForcedPauseAfterSixNativeToolRounds(t *testing.T) {
	svc := batchService(t)
	if err := svc.store.SaveBinanceConnection("demo", "fake-key", "fake-secret"); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.SaveModel("test", "test", "test", "test"); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.store.Prompts()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := cfg["wait"]; exists {
		t.Fatal("retired wait setting should be archived")
	}
	if err := svc.store.SavePrompts(cfg); err != nil {
		t.Fatal(err)
	}
	// Allow Windows SQLite fsync/antivirus variance under repeated suites while
	// remaining well below the removed 30-second forced pause.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	remote := &scriptedRemote{cancel: cancel}
	svc.remote = remote
	svc.newClient = func(string, string, bool) agentClient { return submitFixture() }
	svc.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		return admin.Resolved{Model: "fake"}, nil
	}
	if err := svc.cloudLoop(ctx); err != nil {
		t.Fatal(err)
	}
	if remote.replies != 11 {
		t.Fatalf("loop paused instead of continuing: %d replies", remote.replies)
	}
	for _, prompt := range remote.prompts {
		if strings.Contains(prompt, "LEGACY_FORCED_WAIT_MUST_NOT_APPEAR") {
			t.Fatal("retired reminder still injected")
		}
	}
	for _, line := range svc.Log() {
		if strings.Contains(line.Text, "先停 30 秒") {
			t.Fatal("forced pause persisted")
		}
	}
}

func TestStartRejectsActivePanelReplyOrFinishingLoop(t *testing.T) {
	svc := batchService(t)
	for _, kind := range []string{"reply", "scan", "loop"} {
		svc.replying.Store(kind == "reply")
		svc.scanning.Store(kind == "scan")
		svc.loopActive.Store(kind == "loop")
		if !errors.Is(svc.Start(), ErrBusy) {
			t.Fatalf("%s allowed competing execution loops", kind)
		}
	}
	svc.replying.Store(false)
	svc.scanning.Store(false)
	svc.loopActive.Store(false)
}

func TestPanelWaitReturnsAnExplicitUnscheduledResult(t *testing.T) {
	s := batchService(t)
	mem, waited, _ := s.runToolCalls(context.Background(), &agentState{service: s}, []toolCall{{"wait", `{"seconds":900}`}}, false)
	if waited != 0 || len(mem) != 1 || !strings.Contains(mem[0].Text, `"scheduled":false`) {
		t.Fatal("panel wait was silent or scheduled", mem, waited)
	}
	lines := s.Log()
	if len(lines) != 2 || lines[0].Role != "tool" || lines[1].Role != "result" || lines[1].Name != "wait" {
		t.Fatal("panel cannot pair wait request/result", lines)
	}
}
