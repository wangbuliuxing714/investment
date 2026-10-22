package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"desk/internal/ai"
	"desk/internal/history"
	"desk/internal/opencode"
	"desk/internal/store"

	"github.com/joho/godotenv"
)

type boundedIntegrityRemote struct {
	remoteAgent
	rounds int
}

func (r *boundedIntegrityRemote) Round(ctx context.Context, in opencode.Input, msgs []json.RawMessage, tools []any) (ai.Turn, error) {
	r.rounds++
	if r.rounds > 8 {
		return ai.Turn{}, fmt.Errorf("integrity verification exceeded eight model requests")
	}
	return r.remoteAgent.Round(ctx, in, msgs, tools)
}

// Uses the selected real model and real local market archive, but all chat and
// file writes go to a temporary copy of an online SQLite backup. No trading
// client can be created in this optimization-only test.
func TestLiveToolIntegrityOnIsolatedWorkspace(t *testing.T) {
	if os.Getenv("DESK_TOOL_INTEGRITY_LIVE") != "1" {
		t.Skip("opt-in real model verification")
	}
	_ = godotenv.Load(filepath.Join("..", "..", ".env"))
	snapshot := os.Getenv("DESK_TOOL_INTEGRITY_SNAPSHOT")
	data, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "desk.db")
	if err = os.WriteFile(dbPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if _, err = s.SwitchMode(ctx, "optimization", s.conversation.ID); err != nil {
		t.Fatal(err)
	}
	h, err := history.Open(filepath.Join("..", "..", "data", "market.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s.AttachHistory(h)
	r := &boundedIntegrityRemote{remoteAgent: nativeAgent{}}
	s.remote = r
	s.newClient = func(string, string, bool) agentClient {
		panic("integrity verification must never create a trading client")
	}
	before, err := st.WorkspaceRead("INVEST.MD")
	if err != nil {
		t.Fatal(err)
	}
	s.say("user", "", "这是隔离副本的原生工具维护验收。只完成本任务，不继续研究，不调用 wait。请用本轮原生工具：1. list_files 列出根目录，并 read_file 读取 INVEST.MD 和验证.md；2. get_history 查询 BTCUSDT um 1d limit=100 end=1577145600，以及 um 1d limit=200 end=1748736000，以及 um 1M limit=10 end=0；3. 保留验证.md 原文，在末尾追加一段 Markdown：## 原生工具验收\n中文正文完整性校验。用刚读到的 revision 保存；4. 精确 read_file 读回验证.md 核对，最后 list_files 再列根目录。不要创建新文件，不要修改 INVEST.MD 和复盘.md。参数用工具定义规定的原生调用，不在回复正文里写调用 JSON。完成后简短报告实际验收结果。")
	startID := s.seq
	if err = s.answer(ctx); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	historyCounts := map[int64]int{}
	for _, line := range s.Log() {
		if line.ID <= startID || line.Role != "result" {
			continue
		}
		counts[line.Name]++
		var result struct {
			Error   string `json:"error"`
			Count   int    `json:"count"`
			Request struct {
				End int64 `json:"end"`
			} `json:"request"`
		}
		if line.Name != "list_files" {
			if err = json.Unmarshal([]byte(line.Text), &result); err != nil || result.Error != "" {
				t.Fatalf("%s failed: json=%v tool=%s", line.Name, err, result.Error)
			}
		}
		if line.Name == "get_history" {
			historyCounts[result.Request.End] = result.Count
		}
	}
	if counts["list_files"] < 2 || counts["read_file"] < 2 || counts["write_file"] != 1 || historyCounts[1577145600] != 100 || historyCounts[1748736000] != 200 || historyCounts[0] != 10 {
		t.Fatalf("incomplete actual tool execution: rounds=%d tools=%v historicalCounts=%v", r.rounds, counts, historyCounts)
	}
	after, _ := st.WorkspaceRead("INVEST.MD")
	file, err := st.WorkspaceRead("验证.md")
	if after != before || err != nil || file.Revision < 2 || !strings.Contains(file.Content, "中文正文完整性校验") {
		t.Fatal("existing research changed or verification file not stored", err)
	}
	t.Logf("real model rounds=%d actual tools=%v history counts=%v existing research unchanged", r.rounds, counts, historyCounts)
}
