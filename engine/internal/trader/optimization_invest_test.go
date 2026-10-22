package trader

import (
	"context"
	"desk/internal/admin"
	"desk/internal/prompts"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOptimizationDocumentRefreshesNextRequest(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		name := "panel"
		if continuous {
			name = "continuous"
		}
		t.Run(name, func(t *testing.T) {
			s := batchService(t)
			if _, err := s.SwitchMode(context.Background(), "optimization", "trading"); err != nil {
				t.Fatal(err)
			}
			if err := s.store.SaveModel("fake", "fake", "fake", "fake"); err != nil {
				t.Fatal(err)
			}
			f, err := s.modeInvest()
			if err != nil {
				t.Fatal(err)
			}
			f, err = s.store.WorkspaceWrite(f.Path, "CUSTOM_BEFORE {{coins}}", false, f.Revision)
			if err != nil {
				t.Fatal(err)
			}
			args, _ := json.Marshal(map[string]any{"name": "write_file", "arguments": map[string]any{"path": f.Path, "revision": f.Revision, "content": "CUSTOM_AFTER {{maxPositions}}"}})
			remote := &investRemote{replies: []string{"```tool\n" + string(args) + "\n```", "done"}}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if continuous {
				remote.afterSay = func(round int) {
					if round == 2 {
						cancel()
					}
				}
			}
			s.remote = remote
			s.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
				return admin.Resolved{Model: "fake"}, nil
			}
			s.newClient = func(string, string, bool) agentClient {
				t.Error("optimization constructed a trading client")
				return nil
			}
			if continuous {
				err = s.cloudLoop(ctx)
			} else {
				err = s.answer(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(remote.systems) != 2 || !strings.Contains(remote.systems[0], "CUSTOM_BEFORE BTCUSDT, ETHUSDT") || !strings.Contains(remote.systems[1], "CUSTOM_AFTER 2") || strings.Contains(remote.systems[1], "CUSTOM_BEFORE") {
				t.Fatal(remote.systems)
			}
			cfg, _ := s.store.Prompts()
			tools, _ := s.store.Tools()
			for _, system := range remote.systems {
				if !strings.Contains(system, cfg["system"]) || !strings.Contains(system, cfg["optimization"]) || !strings.Contains(system, tools.Instructions) || strings.Contains(system, cfg["trading"]) {
					t.Fatal("saved shared, mode and tool instructions not selected correctly")
				}
			}
			if continuous && (!strings.Contains(remote.requests[0], prompts.StartEvent) || strings.Contains(remote.requests[0], "先检查")) {
				t.Fatal(remote.requests[0])
			}
		})
	}
}
