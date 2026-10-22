package trader

import (
	"context"
	"strings"
	"testing"
	"time"

	"desk/internal/admin"
	"desk/internal/opencode"
)

type systemRefreshRemote struct {
	systems  []string
	requests []string
	onSay    func(int)
}

func (r *systemRefreshRemote) Prepare(_ context.Context, in opencode.Input) error {
	r.systems = append(r.systems, in.SystemPrompt)
	return nil
}
func (r *systemRefreshRemote) Say(_ context.Context, _, prompt string) (string, error) {
	r.requests = append(r.requests, prompt)
	r.onSay(len(r.requests))
	return `{"name":"list_files","arguments":{}}`, nil
}

func TestRenderedSystemRefreshesWhenStrategyToolsOrPromptChanges(t *testing.T) {
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
	cfg["system"] = "SYSTEM_ONLY max={{maxPositions}}"
	if err = svc.store.SavePrompts(cfg); err != nil {
		t.Fatal(err)
	}
	st, err := svc.store.Strategy()
	if err != nil {
		t.Fatal(err)
	}
	st.MaxPositions = 2
	if err = svc.store.SaveStrategy(st); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	remote := &systemRefreshRemote{}
	remote.onSay = func(round int) {
		switch round {
		case 1:
			st.MaxPositions = 3
			if err := svc.store.SaveStrategy(st); err != nil {
				t.Fatal(err)
			}
		case 2:
			tools, err := svc.store.Tools()
			if err != nil {
				t.Fatal(err)
			}
			tools.Instructions += "\nTOOLS_CHANGED max={{maxPositions}}"
			if err = svc.store.SaveTools(tools); err != nil {
				t.Fatal(err)
			}
		case 3:
			cfg["system"] += "\nSYSTEM_UPDATED"
			if err := svc.store.SavePrompts(cfg); err != nil {
				t.Fatal(err)
			}
		case 5:
			cancel()
		}
	}
	svc.remote = remote
	svc.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		return admin.Resolved{Model: "fake"}, nil
	}
	if err = svc.cloudLoop(ctx); err != nil {
		t.Fatal(err)
	}
	if len(remote.requests) != 5 || len(remote.systems) != 4 {
		t.Fatalf("unexpected refreshes: %d systems for %d requests", len(remote.systems), len(remote.requests))
	}
	for i, want := range []string{"SYSTEM_ONLY max=2", "SYSTEM_ONLY max=3", "TOOLS_CHANGED max=3", "SYSTEM_UPDATED"} {
		if !strings.Contains(remote.systems[i], want) || strings.Contains(remote.systems[i], "{{maxPositions}}") {
			t.Fatalf("system %d not rendered/refreshed: %s", i, remote.systems[i])
		}
	}
	for _, request := range remote.requests {
		if strings.Contains(request, "SYSTEM_ONLY") || strings.Contains(request, "TOOLS_CHANGED") || strings.Contains(request, "SYSTEM_UPDATED") {
			t.Fatal("system context duplicated in request body")
		}
	}
}
