package trader

import (
	"context"
	"desk/internal/ai"
	"strings"
	"testing"
)

func TestOptimizationNativeQueriesThenFinalReplyStopsNormally(t *testing.T) {
	rounds := 0
	s := nativeTestService(t, func(req nativeRequest) any {
		rounds++
		if rounds == 1 {
			return nativeReply("列出文件并读取策略。", ai.ToolCall{ID: "list", Name: "list_files", Arguments: `{}`}, ai.ToolCall{ID: "read", Name: "read_file", Arguments: `{"path":"INVEST.MD"}`})
		}
		results := nativeResults(t, req)
		if !strings.Contains(results["list"], "INVEST.MD") || !strings.Contains(results["read"], "sha256") {
			t.Error("results did not reach model")
		}
		return nativeReply("本轮检查完成。")
	})
	if err := s.store.SetRunning(true); err != nil {
		t.Fatal(err)
	}
	if err := s.cloudLoop(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, _ := s.State()
	if rounds != 2 || state.Running || state.StopKind != "completed" {
		t.Fatal(rounds, state)
	}
	for _, line := range s.Log() {
		if strings.Contains(line.Text, "未识别到工具调用") {
			t.Fatal(line)
		}
	}
}
