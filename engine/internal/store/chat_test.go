package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestChatLogSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("BTCUSDT 15m 最新价 1\n", 400)
	id, err := st.AppendChat("result", "get_market", body, "2026-09-29T00:00:00Z")
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	if _, err := st.AppendChat("tool", "get_market", `查看行情 {"symbol":"BTCUSDT"}`, "2026-09-29T00:00:01Z"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	rows, err := again.RecentChat(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Role != "result" || rows[0].Text != body || rows[1].Name != "get_market" {
		t.Fatalf("reloaded %#v", rows)
	}
}

func TestChatLogKeepsNewest(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := 0; i < ChatLogKeep+3; i++ {
		if _, err := st.AppendChat("status", "", "第几条", "2026-09-29T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := st.RecentChat(ChatLogKeep + 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != ChatLogKeep {
		t.Fatalf("kept %d", len(rows))
	}
	if rows[0].ID+int64(ChatLogKeep-1) != rows[len(rows)-1].ID {
		t.Fatalf("ids %d..%d", rows[0].ID, rows[len(rows)-1].ID)
	}
}
