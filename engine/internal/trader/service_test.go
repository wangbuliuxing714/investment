package trader

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"desk/internal/store"
)

func TestConfigureExchangeRejectsUnfinishedWork(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(st, nil)
	called := false
	update := func() error { called = true; return nil }
	for _, flag := range []string{"loop", "reply", "scan"} {
		svc.loopActive.Store(flag == "loop")
		svc.replying.Store(flag == "reply")
		svc.scanning.Store(flag == "scan")
		if err := svc.ConfigureExchange(update); !errors.Is(err, ErrExchangeBusy) || called {
			t.Fatalf("%s allowed configuration while work was unfinished", flag)
		}
	}
	svc.loopActive.Store(false)
	svc.replying.Store(false)
	svc.scanning.Store(false)
	if err := svc.ConfigureExchange(update); err != nil || !called {
		t.Fatal("idle service should allow configuration")
	}
}

func TestSayPersistsToolResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st, nil)
	market := strings.Repeat("ETHUSDT 15m 最新价 2 EMA20 2 MACD 0 信号 0 柱 0 RSI7 50\n2026-09-29 00:00 1 2 0.5 1.5 3\n", 200)
	if len(market) < 7000 {
		t.Fatalf("fixture too short: %d", len(market))
	}
	svc.say("tool", "get_market", `查看行情 {"symbol":"ETHUSDT"}`)
	svc.say("result", "get_market", market)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	reloaded := New(again, nil)
	lines := reloaded.Log()
	if len(lines) != 2 {
		t.Fatalf("lines %d", len(lines))
	}
	if lines[0].Role != "tool" || lines[0].Name != "get_market" {
		t.Fatalf("tool line %#v", lines[0])
	}
	if lines[1].Role != "result" || lines[1].Text != strings.TrimSpace(market) {
		t.Fatalf("result len %d", len(lines[1].Text))
	}
	mem := reloaded.transcriptFromLog()
	if len(mem) != 2 || mem[1].Text != strings.TrimSpace(market) {
		t.Fatal("model replay lost later candles while panel kept them")
	}
	if !strings.Contains(transcriptText(mem, "history"), strings.TrimSpace(market)) {
		t.Fatal("model request truncated the market result")
	}
}

func TestClipChatTextKeepsMarket(t *testing.T) {
	long := strings.Repeat("a", 8000)
	if got := clipChatText("result", long); got != long {
		t.Fatalf("result clipped to %d", len(got))
	}
	if got := clipChatText("status", long); len(got) <= 6000 || !strings.HasSuffix(got, "…") {
		t.Fatalf("status %d", len(got))
	}
}
