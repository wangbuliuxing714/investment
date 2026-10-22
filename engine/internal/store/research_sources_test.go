package store

import (
	"desk/internal/agenttools"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestResearchSourceMigrationBacksUpStockAndPreservesCustom(t *testing.T) {
	old := agenttools.LegacyInstructions + "\n\n" + agenttools.LegacyAnalyticsInstructions
	for _, guide := range []string{old, old + "\n\n" + agenttools.DecisionInstructions, "custom guide with explicit active", old + "\nmy custom rule"} {
		path := filepath.Join(t.TempDir(), "desk.db")
		s, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = s.SaveTools(agenttools.Config{Instructions: guide}); e != nil {
			t.Fatal(e)
		}
		s.Close()
		s, e = Open(path)
		if e != nil {
			t.Fatal(e)
		}
		stock := guide == old || guide == old+"\n\n"+agenttools.DecisionInstructions
		got, _ := s.Tools()
		if stock {
			if got.Instructions != agenttools.DefaultInstructions {
				t.Fatal("stock not upgraded")
			}
			var raw string
			if e = s.db.QueryRow("SELECT value FROM settings WHERE key='agent_tools_before_research_sources'").Scan(&raw); e != nil {
				t.Fatal(e)
			}
			var backup agenttools.Config
			if e = json.Unmarshal([]byte(raw), &backup); e != nil || backup.Instructions != guide {
				t.Fatal("bad backup")
			}
		} else if got.Instructions != guide {
			t.Fatal("custom settings overwritten")
		}
		if e = s.upgradeResearchSourceTools(); e != nil {
			t.Fatal(e)
		}
		again, _ := s.Tools()
		if got != again {
			t.Fatal("migration not idempotent")
		}
		s.Close()
	}
}
