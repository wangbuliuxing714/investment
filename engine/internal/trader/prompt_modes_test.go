package trader

import (
	"context"
	"desk/internal/admin"
	"desk/internal/ai"
	"desk/internal/opencode"
	"encoding/json"
	"strings"
	"testing"
)

type modePromptRemote struct {
	systems []string
	after   func(int)
}

func (r *modePromptRemote) Round(_ context.Context, in opencode.Input, _ []json.RawMessage, _ []any) (ai.Turn, error) {
	r.systems = append(r.systems, in.SystemPrompt)
	r.after(len(r.systems))
	text := `{"choices":[{"message":{"role":"assistant","content":"done"}}]}`
	if len(r.systems) == 1 {
		text = `{"choices":[{"message":{"role":"assistant","content":"read","tool_calls":[{"id":"mode_read_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"INVEST.MD\"}"}}]}}]}`
	}
	return ai.ParseTurn([]byte(text))
}

func TestSelectedModeAndSharedRulesRefreshInBothInvocationPaths(t *testing.T) {
	for _, mode := range []string{"trading", "optimization"} {
		for _, automatic := range []bool{false, true} {
			name := mode + "/panel"
			if automatic {
				name = mode + "/continuous"
			}
			t.Run(name, func(t *testing.T) {
				s := batchService(t)
				if mode == "optimization" {
					if _, err := s.SwitchMode(context.Background(), mode, "trading"); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.store.SaveModel("fake", "fake", "fake", "fake"); err != nil {
					t.Fatal(err)
				}
				if automatic && mode == "trading" {
					if err := s.store.SaveBinanceConnection("demo", "fake", "fake"); err != nil {
						t.Fatal(err)
					}
				}
				cfg, _ := s.store.Prompts()
				cfg["system"], cfg["trading"], cfg["optimization"], cfg["chat"] = "COMMON_BEFORE", "TRADING_ONLY {{maxPositions}}", "RESEARCH_ONLY {{maxPositions}}", "PANEL_ONLY"
				if err := s.store.SavePrompts(cfg); err != nil {
					t.Fatal(err)
				}
				f, _ := s.store.Invest()
				if _, err := s.store.WorkspaceWrite(f.Path, "STRATEGY_BEFORE", false, f.Revision); err != nil {
					t.Fatal(err)
				}
				tools, _ := s.store.Tools()
				tools.Instructions = "SAVED_TOOLS_BEFORE"
				if err := s.store.SaveTools(tools); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				r := &modePromptRemote{}
				r.after = func(round int) {
					if round == 1 {
						cfg["system"] = "COMMON_AFTER"
						cfg[mode] += " MODE_UPDATED"
						if err := s.store.SavePrompts(cfg); err != nil {
							t.Fatal(err)
						}
						f, _ := s.store.Invest()
						if _, err := s.store.WorkspaceWrite(f.Path, "STRATEGY_AFTER", false, f.Revision); err != nil {
							t.Fatal(err)
						}
						tools.Instructions = "SAVED_TOOLS_AFTER"
						if err := s.store.SaveTools(tools); err != nil {
							t.Fatal(err)
						}
					} else if automatic {
						cancel()
					}
				}
				s.remote = r
				s.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
					return admin.Resolved{Model: "fake"}, nil
				}
				s.newClient = func(string, string, bool) agentClient {
					if mode == "optimization" {
						t.Fatal("optimization created exchange client")
					}
					return submitFixture()
				}
				var err error
				if automatic {
					err = s.cloudLoop(ctx)
				} else {
					err = s.answer(ctx)
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(r.systems) != 2 {
					t.Fatal("missing second request", len(r.systems))
				}
				selected, opposite := "TRADING_ONLY 2", "RESEARCH_ONLY"
				if mode == "optimization" {
					selected, opposite = "RESEARCH_ONLY 2", "TRADING_ONLY"
				}
				for i, system := range r.systems {
					for _, want := range []string{selected, "当前代理模式：" + mode} {
						if !strings.Contains(system, want) {
							t.Fatal("missing selected saved context", want)
						}
					}
					if strings.Contains(system, opposite) || strings.Contains(system, "{{maxPositions}}") {
						t.Fatal("opposite mode leaked or variables unrendered")
					}
					if strings.Contains(system, "PANEL_ONLY") == automatic {
						t.Fatal("panel rules not selected by invocation path")
					}
					if i == 1 {
						for _, want := range []string{"COMMON_AFTER", "STRATEGY_AFTER", "SAVED_TOOLS_AFTER", "MODE_UPDATED"} {
							if !strings.Contains(system, want) {
								t.Fatal("saved edit not refreshed", want)
							}
						}
						if strings.Contains(system, "BEFORE") {
							t.Fatal("stale saved context")
						}
					}
				}
			})
		}
	}
}
