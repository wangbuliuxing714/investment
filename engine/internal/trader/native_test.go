package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"desk/internal/admin"
	"desk/internal/ai"
	"desk/internal/store"
)

type nativeRequest struct {
	Messages []json.RawMessage `json:"messages"`
	Tools    []struct {
		Function struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
}

func nativeTestService(t *testing.T, handler func(nativeRequest) any) *Service {
	t.Helper()
	s := batchService(t)
	if _, err := s.SwitchMode(context.Background(), "optimization", "trading"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SaveModel("fake", "fake", "fake", "fake"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"验证.md", "复盘.md"} {
		if _, err := s.store.WorkspaceRead(path); err != nil {
			if _, err = s.store.WorkspaceWrite(path, "# 验收初稿", false, 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req nativeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(handler(req))
	}))
	t.Cleanup(server.Close)
	s.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		return admin.Resolved{APIURL: server.URL, APIKey: "test", Model: "fake"}, nil
	}
	s.newClient = func(string, string, bool) agentClient { panic("optimization must never construct exchange client") }
	s.say("user", "", "执行验收")
	return s
}
func nativeReply(content string, calls ...ai.ToolCall) any {
	native := []any{}
	for _, c := range calls {
		native = append(native, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": c.Arguments}})
	}
	return map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content, "tool_calls": native}}}}
}
func nativeResults(t *testing.T, req nativeRequest) map[string]string {
	t.Helper()
	out := map[string]string{}
	pending := map[string]bool{}
	for _, raw := range req.Messages {
		var m struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			ID      string `json:"tool_call_id"`
			Calls   []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if m.Role == "tool" {
			if !pending[m.ID] {
				t.Fatalf("orphan result: %s", m.ID)
			}
			delete(pending, m.ID)
			out[m.ID] = m.Content
		}
		if m.Role == "assistant" {
			if len(pending) > 0 {
				t.Fatalf("missing results: %v", pending)
			}
			for _, c := range m.Calls {
				pending[c.ID] = true
			}
		}
	}
	if len(pending) > 0 {
		t.Fatalf("unpaired calls: %v", pending)
	}
	return out
}

func TestNativeHTTPFileWorkflowPairsIDsAndSurvivesNewService(t *testing.T) {
	text := "# 中文完整正文\n\n" + strings.Repeat("每次问题、原因、修正与复测。\n", 2200) + "结尾校验✓"
	var svc *Service
	rounds := 0
	var revision int
	svc = nativeTestService(t, func(req nativeRequest) any {
		rounds++
		for _, tool := range req.Tools {
			if tool.Function.Name == "get_account" || tool.Function.Name == "submit_decisions" || tool.Function.Name == "get_market" || tool.Function.Name == "wait" || tool.Function.Name == "create_directory" {
				t.Errorf("forbidden schema %s", tool.Function.Name)
			}
		}
		results := nativeResults(t, req)
		switch rounds {
		case 1:
			return nativeReply("读取两份文件", ai.ToolCall{ID: "read_a", Name: "read_file", Arguments: `{"path":"验证.md"}`}, ai.ToolCall{ID: "read_b", Name: "read_file", Arguments: `{"path":"INVEST.MD"}`})
		case 2:
			if results["read_a"] == "" || !strings.Contains(results["read_b"], "INVEST.MD") {
				t.Error("missing same-name paired results")
			}
			var f store.WorkspaceFile
			if err := json.Unmarshal([]byte(results["read_a"]), &f); err != nil {
				t.Error(err)
			}
			// Read result is an envelope; the actual revision is read locally for
			// fixture construction and verified independently on the response.
			file, err := svc.store.WorkspaceRead("验证.md")
			if err != nil {
				t.Error(err)
			}
			revision = file.Revision
			args, _ := json.Marshal(map[string]any{"path": "验证.md", "revision": revision, "content": text})
			return nativeReply("保存", ai.ToolCall{ID: "write", Name: "write_file", Arguments: string(args)})
		case 3:
			if !strings.Contains(results["write"], `"revision"`) {
				t.Error("missing write result")
			}
			return nativeReply("读回", ai.ToolCall{ID: "verify", Name: "read_file", Arguments: `{"path":"验证.md"}`})
		default:
			var result map[string]any
			json.Unmarshal([]byte(results["verify"]), &result)
			if !strings.Contains(results["verify"], "结尾校验✓") {
				t.Error("long Markdown response was truncated")
			}
			return nativeReply("**验收完成**")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := svc.answer(ctx); err != nil {
		t.Fatal(err)
	}
	f, err := svc.store.WorkspaceRead("验证.md")
	if err != nil || f.Content != text || f.Revision != revision+1 {
		t.Fatal("write/read mismatch", err)
	}
	if rounds != 4 {
		t.Fatal(rounds)
	}
	// A fresh service reads completed structured batches, without executing
	// their historical write again. All IDs and full tool results survive.
	next := New(svc.store, nil)
	next.remote = svc.remote
	next.resolveModel = svc.resolveModel
	next.newClient = svc.newClient
	if err = next.answer(ctx); err != nil {
		t.Fatal(err)
	}
	f, _ = svc.store.WorkspaceRead("验证.md")
	if f.Revision != revision+1 {
		t.Fatal("historical mutation replayed")
	}
}

func TestNativeRejectsProseCallsAndCompletesContinuousRun(t *testing.T) {
	rounds := 0
	s := nativeTestService(t, func(req nativeRequest) any {
		rounds++
		return nativeReply("```tool\n{\"name\":\"write_file\",\"arguments\":{\"path\":\"验证.md\",\"revision\":1,\"content\":\"伪调用\"}}\n```")
	})
	before, _ := s.store.WorkspaceRead("验证.md")
	if err := s.store.SetRunning(true); err != nil {
		t.Fatal(err)
	}
	if err := s.cloudLoop(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := s.store.WorkspaceRead("验证.md")
	state, _ := s.State()
	if before != after || rounds != 1 || state.Running {
		t.Fatal("prose executed or completion spun", rounds, state)
	}
	for _, line := range s.Log() {
		if strings.Contains(line.Text, "未识别到工具调用") {
			t.Fatal(line)
		}
	}
}

func TestNativeErrorsAreReturnedToExactCallAndCannotMutate(t *testing.T) {
	rounds := 0
	s := nativeTestService(t, func(req nativeRequest) any {
		rounds++
		if rounds == 1 {
			return nativeReply("", ai.ToolCall{ID: "bad_json", Name: "write_file", Arguments: `{"path":`}, ai.ToolCall{ID: "alias", Name: "读取 Markdown", Arguments: `{"path":"INVEST.MD"}`}, ai.ToolCall{ID: "denied", Name: "submit_decisions", Arguments: `{"decisions":[]}`}, ai.ToolCall{ID: "wait_panel", Name: "wait", Arguments: `{}`}, ai.ToolCall{ID: "missing_revision", Name: "write_file", Arguments: `{"path":"验证.md","content":"bad"}`}, ai.ToolCall{ID: "good", Name: "list_files", Arguments: `{}`})
		}
		results := nativeResults(t, req)
		for _, id := range []string{"bad_json", "alias", "denied", "wait_panel", "missing_revision"} {
			if !strings.Contains(results[id], `"executed":false`) {
				t.Errorf("%s: %s", id, results[id])
			}
		}
		if !strings.Contains(results["good"], "INVEST.MD") {
			t.Error(results["good"])
		}
		return nativeReply("已报告错误")
	})
	before, _ := s.store.WorkspaceRead("验证.md")
	if err := s.answer(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := s.store.WorkspaceRead("验证.md")
	if before != after || rounds != 2 {
		t.Fatal("invalid calls changed files")
	}
}

func TestNativeUnavailableChannelDoesNotFallbackToText(t *testing.T) {
	s := nativeTestService(t, func(nativeRequest) any {
		return map[string]any{"error": map[string]any{"message": "tools is not supported"}}
	})
	err := s.answer(context.Background())
	if err == nil || !strings.Contains(err.Error(), ai.ErrToolsUnsupported.Error()) {
		t.Fatal(err)
	}
	for _, line := range s.Log() {
		if line.Role == "tool" {
			t.Fatal("fallback executed", fmt.Sprint(line))
		}
	}
}

func TestNativeWaitResultPairsAndUserWakesWithoutRepeatingCalls(t *testing.T) {
	var s *Service
	rounds := 0
	s = nativeTestService(t, func(req nativeRequest) any {
		rounds++
		if rounds == 1 {
			s.say("user", "", "现在继续")
			s.mu.Lock()
			s.notes = append(s.notes, "现在继续")
			s.mu.Unlock()
			s.nudge()
			return nativeReply("等待新任务", ai.ToolCall{ID: "schedule", Name: "wait", Arguments: `{"seconds":900,"reason":"等新任务"}`})
		}
		results := nativeResults(t, req)
		if !strings.Contains(results["schedule"], `"scheduled":true`) {
			t.Error(results)
		}
		if !strings.Contains(string(req.Messages[len(req.Messages)-1]), "现在继续") {
			t.Error("wake message missing")
		}
		return nativeReply("已收到，完成。")
	})
	if err := s.store.SetRunning(true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.cloudLoop(ctx); err != nil {
		t.Fatal(err)
	}
	if rounds != 2 || ctx.Err() != nil {
		t.Fatal("wait was not woken", rounds, ctx.Err())
	}
	frames, err := s.store.NativeFrames(s.conversationID())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, f := range frames {
		for _, raw := range f.Messages {
			if strings.Contains(string(raw), `"tool_call_id":"schedule"`) {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatal("wait result lost or repeated", count)
	}
}

func TestNativeInterruptedMutationRecoversAsUnknownWithoutReplay(t *testing.T) {
	rounds := 0
	s := nativeTestService(t, func(req nativeRequest) any {
		rounds++
		results := nativeResults(t, req)
		if !strings.Contains(results["uncertain_write"], `"executionStatus":"unknown"`) {
			t.Error("missing unknown execution state", results)
		}
		if !strings.Contains(results["completed_read"], "confirmed engine result") {
			t.Error("lost completed result")
		}
		return nativeReply("上次执行状态未知，未重复保存。")
	})
	before, _ := s.store.WorkspaceRead("验证.md")
	turn, err := ai.ParseTurn(mustJSON(t, nativeReply("", ai.ToolCall{ID: "completed_read", Name: "read_file", Arguments: `{"path":"验证.md"}`}, ai.ToolCall{ID: "uncertain_write", Name: "write_file", Arguments: `{"path":"验证.md","revision":1,"content":"unexpected replay"}`})))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.SaveNativePending(s.conversationID(), store.NativeFrame{Messages: []json.RawMessage{turn.Raw, ai.ToolResult("completed_read", "confirmed engine result")}}); err != nil {
		t.Fatal(err)
	}
	if err = s.answer(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := s.store.WorkspaceRead("验证.md")
	if after != before || rounds != 1 {
		t.Fatal("interrupted write was replayed")
	}
	pending, err := s.store.NativePending(s.conversationID())
	if err != nil || len(pending.Messages) != 0 {
		t.Fatal("recovery not committed", err)
	}
}
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
