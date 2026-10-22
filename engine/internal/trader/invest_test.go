package trader

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"desk/internal/admin"
	"desk/internal/opencode"
)

type investRemote struct {
	systems  []string
	requests []string
	replies  []string
	afterSay func(int)
}

func (r *investRemote) Prepare(_ context.Context, in opencode.Input) error {
	r.systems = append(r.systems, in.SystemPrompt)
	return nil
}
func (r *investRemote) Say(_ context.Context, _, prompt string) (string, error) {
	r.requests = append(r.requests, prompt)
	if r.afterSay != nil {
		r.afterSay(len(r.requests))
	}
	return r.replies[len(r.requests)-1], nil
}

func TestInvestToolEditRefreshesNextPanelRequest(t *testing.T) {
	s := batchService(t)
	if err := s.store.SaveModel("fake", "fake", "fake", "fake"); err != nil {
		t.Fatal(err)
	}
	f, _ := s.store.Invest()
	if _, err := s.store.WorkspaceWrite(f.Path, "INVEST_BEFORE {{coins}}", false, f.Revision); err != nil {
		t.Fatal(err)
	}
	f, _ = s.store.Invest()
	args, _ := json.Marshal(map[string]any{"name": "write_file", "arguments": map[string]any{"path": f.Path, "revision": f.Revision, "content": "INVEST_AFTER {{maxPositions}}"}})
	r := &investRemote{replies: []string{"```tool\n" + string(args) + "\n```", "done"}}
	s.remote = r
	s.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		return admin.Resolved{Model: "fake"}, nil
	}
	if err := s.answer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.systems) != 2 || strings.Count(r.systems[0], "INVEST_BEFORE BTCUSDT, ETHUSDT") != 1 || !strings.Contains(r.systems[1], "INVEST_AFTER 2") || strings.Contains(r.systems[1], "INVEST_BEFORE") {
		t.Fatalf("panel did not refresh injected instructions: %+v", r.systems)
	}
}

func TestInvestRefreshesInContinuousRunWithEventOnlyNotifications(t *testing.T) {
	s := batchService(t)
	if err := s.store.SaveModel("fake", "fake", "fake", "fake"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveBinanceConnection("demo", "fake", "fake"); err != nil {
		t.Fatal(err)
	}
	f, _ := s.store.Invest()
	if _, err := s.store.WorkspaceWrite(f.Path, "INVEST_FIRST {{coins}}", false, f.Revision); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := &investRemote{replies: []string{`{"name":"list_files","arguments":{}}`, "finished"}}
	r.afterSay = func(round int) {
		if round == 1 {
			f, _ := s.store.Invest()
			if _, err := s.store.WorkspaceWrite(f.Path, "INVEST_SECOND {{maxPositions}}", false, f.Revision); err != nil {
				t.Fatal(err)
			}
		} else {
			cancel()
		}
	}
	s.remote = r
	s.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		return admin.Resolved{Model: "fake"}, nil
	}
	if err := s.cloudLoop(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.systems) != 2 || !strings.Contains(r.systems[0], "INVEST_FIRST BTCUSDT, ETHUSDT") || !strings.Contains(r.systems[1], "INVEST_SECOND 2") {
		t.Fatal("continuous context did not refresh", r.systems)
	}
	if !strings.Contains(r.requests[0], "持续运行已启动") || !strings.Contains(r.requests[1], "INVEST.MD") {
		t.Fatal("missing event notifications", r.requests)
	}
	for _, request := range r.requests {
		if strings.Contains(request, "INVEST_FIRST") || strings.Contains(request, "INVEST_SECOND") {
			t.Fatal("injected policy duplicated in request body")
		}
	}
}
