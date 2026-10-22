package store

import (
	"desk/internal/agenttools"
	"desk/internal/prompts"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestToolMigrationPreservesOriginalAndIndependentSaves(t *testing.T) {
	for _, strategy := range []string{"", "original strategy"} {
		path := filepath.Join(t.TempDir(), "desk.db")
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		legacy := prompts.LegacyDefaults()
		legacy["system"] = "custom system"
		legacy["strategy"] = strategy
		legacy["tools"] = "  原始工具说明\n{{coins}}\n"
		raw, _ := json.Marshal(legacy)
		if _, err = s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('agent_tools','model_prompts_structure_v2')"); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		tools, err := s.Tools()
		expected, protocol := prompts.Consolidate(legacy)
		if err != nil || tools.Instructions != legacy["tools"]+"\n\n"+protocol {
			t.Fatal("original tools lost", err)
		}
		saved, err := s.Prompts()
		expected, _ = prompts.SplitInvest(expected)
		delete(legacy, "tools")
		if err != nil || saved["chat"] != expected["chat"] || saved["trading"] == "" || saved["optimization"] == "" {
			t.Fatal("prompt migration changed unrelated values", err)
		}
		var backup string
		if err = s.db.QueryRow("SELECT value FROM settings WHERE key='model_prompts_before_tools_split'").Scan(&backup); err != nil || backup != string(raw) {
			t.Fatal("missing original backup", err)
		}
		tools.Instructions = "new independent tools"
		if err = s.SaveTools(tools); err != nil {
			t.Fatal(err)
		}
		saved["system"] = "updated system"
		if err = s.SavePrompts(saved); err != nil {
			t.Fatal(err)
		}
		// A stale client may still send the old combined document; reject it.
		stale := prompts.Defaults()
		stale["tools"] = "stale tools"
		if err = s.SavePrompts(stale); err == nil {
			t.Fatal("stale combined config accepted")
		}
		s.Close()
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		gotTools, _ := s.Tools()
		gotPrompts, _ := s.Prompts()
		if gotTools != tools || !reflect.DeepEqual(gotPrompts, saved) {
			t.Fatal("independent save/reopen overwrote another configuration")
		}
		s.Close()
	}
}

func TestToolSplitDoesNotOverwriteAnExistingConfiguration(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tools := agenttools.Config{Instructions: "authoritative separate tools"}
	if err = s.SaveTools(tools); err != nil {
		t.Fatal(err)
	}
	legacy := prompts.Defaults()
	legacy["tools"] = "older combined tools"
	raw, _ := json.Marshal(legacy)
	if _, err = s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw)); err != nil {
		t.Fatal(err)
	}
	if err = s.splitToolSettings(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Tools()
	if got != tools {
		t.Fatal("existing independent tools overwritten")
	}
}

func TestToolSplitRollsBackBothDocuments(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	legacy := prompts.Defaults()
	legacy["tools"] = "original tools"
	raw, _ := json.Marshal(legacy)
	if _, err = s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key='agent_tools'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER fail_tool_split BEFORE UPDATE ON settings WHEN OLD.key='model_prompts' BEGIN SELECT RAISE(ABORT, 'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.splitToolSettings(); err == nil {
		t.Fatal("expected migration error")
	}
	var got string
	if err = s.db.QueryRow("SELECT value FROM settings WHERE key='model_prompts'").Scan(&got); err != nil || got != string(raw) {
		t.Fatal("partial prompt migration", err)
	}
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM settings WHERE key IN ('agent_tools','model_prompts_before_tools_split')").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial tool migration", err)
	}
}
