package trader

// These adapters convert historical test fixtures, never production responses.
// New protocol tests use actual HTTP tool_calls without this fixture helper.
import (
	"context"
	"desk/internal/ai"
	"desk/internal/opencode"
	"encoding/json"
	"fmt"
	"sync"
)

type fixtureRemote interface {
	Prepare(context.Context, opencode.Input) error
	Say(context.Context, string, string) (string, error)
}

var fixtureSystems sync.Map

func fixtureRound(ctx context.Context, r fixtureRemote, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	old, _ := fixtureSystems.Load(r)
	if old != in.SystemPrompt {
		if err := r.Prepare(ctx, in); err != nil {
			return ai.Turn{}, err
		}
		fixtureSystems.Store(r, in.SystemPrompt)
	}
	mem := []memory{}
	for _, raw := range msgs[1:] {
		var m struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		json.Unmarshal(raw, &m)
		role := m.Role
		switch role {
		case "user":
			role = "用户"
		case "assistant":
			role = "代理"
		case "tool":
			role = "本轮工具执行结果"
		}
		mem = append(mem, memory{Role: role, Text: m.Content})
	}
	text, err := r.Say(ctx, in.Model, transcriptText(mem, ""))
	if err != nil {
		return ai.Turn{}, err
	}
	prose, calls, diagnostic := parseToolReply(text)
	if diagnostic != "" && len(calls) == 0 {
		return ai.Turn{}, fmt.Errorf("%s", diagnostic)
	}
	native := []any{}
	for i, c := range calls {
		native = append(native, map[string]any{"id": fmt.Sprintf("fixture_%d", i), "type": "function", "function": map[string]any{"name": c.Name, "arguments": c.Args}})
	}
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": prose, "tool_calls": native}}}})
	return ai.ParseTurn(raw)
}
func (r *investRemote) Round(ctx context.Context, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	return fixtureRound(ctx, r, in, msgs, tools)
}
func (r *systemRefreshRemote) Round(ctx context.Context, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	return fixtureRound(ctx, r, in, msgs, tools)
}
func (r *blockingModeRemote) Round(ctx context.Context, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	return fixtureRound(ctx, r, in, msgs, tools)
}
func (r *optimizationLoopRemote) Round(ctx context.Context, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	return fixtureRound(ctx, r, in, msgs, tools)
}
func (r *scriptedRemote) Round(ctx context.Context, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	return fixtureRound(ctx, r, in, msgs, tools)
}
func (r *fileChatRemote) Round(ctx context.Context, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	return fixtureRound(ctx, r, in, msgs, tools)
}
func (r *historyRemote) Round(ctx context.Context, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	return fixtureRound(ctx, r, in, msgs, tools)
}
