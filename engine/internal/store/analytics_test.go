package store

import (
	"desk/internal/agenttools"
	"path/filepath"
	"testing"
)

func TestAnalyticsGuideMigrationPreservesCustomAndBackup(t *testing.T) {
	for _, old := range []string{agenttools.LegacyInstructions, "my custom instructions"} {
		path := filepath.Join(t.TempDir(), "desk.db")
		s, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.SaveTools(agenttools.Config{Instructions: old}); e != nil {
			t.Fatal(e)
		}
		s.Close()
		s, e = Open(path)
		if e != nil {
			t.Fatal(e)
		}
		got, e := s.Tools()
		if e != nil {
			t.Fatal(e)
		}
		want := old
		if old == agenttools.LegacyInstructions {
			want = agenttools.DefaultInstructions
			var backup string
			if e = s.db.QueryRow("SELECT value FROM settings WHERE key='agent_tools_before_analytics'").Scan(&backup); e != nil || backup == "" {
				t.Fatal("missing backup", e)
			}
		}
		if got.Instructions != want {
			t.Fatal("wrong migration")
		}
		if e = s.upgradeAnalyticsTools(); e != nil {
			t.Fatal(e)
		}
		again, _ := s.Tools()
		if again != got {
			t.Fatal("not idempotent")
		}
		s.Close()
	}
}
