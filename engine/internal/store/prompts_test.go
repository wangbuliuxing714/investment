package store

import (
	"desk/internal/prompts"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPromptMigrationPreservesLegacyAndIsIdempotent(t *testing.T) {
	for _, legacy := range []string{"custom legacy strategy", ""} {
		path := filepath.Join(t.TempDir(), "desk.db")
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.Exec("DELETE FROM settings WHERE key='model_prompts'"); err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.Exec("UPDATE strategy SET prompt=? WHERE id=1", legacy); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.Prompts()
		invest, fileErr := s.Invest()
		if err != nil || fileErr != nil || (legacy != "" && !strings.Contains(invest.Content, legacy)) {
			t.Fatalf("migration lost custom text: %v", err)
		}
		c["system"] = "saved custom system"
		if err = s.SavePrompts(c); err != nil {
			t.Fatal(err)
		}
		st, _ := s.Strategy()
		st.Prompt = "stale browser data"
		if err = s.SaveStrategy(st); err != nil {
			t.Fatal(err)
		}
		if err = s.SaveModel("model-b", "B", "provider-b", "upstream-b"); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Prompts()
		if err != nil || got["system"] != c["system"] {
			t.Fatal("reopen/model/strategy update overwrote prompts")
		}
		s.Close()
	}
}

func TestStructuredPromptMigrationBacksUpAndRunsOnce(t *testing.T) {
	for _, strategy := range []string{"", "custom legacy strategy"} {
		path := filepath.Join(t.TempDir(), "desk.db")
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		old := prompts.Defaults()
		old["resume"] = "等待结束。继续看盘，并自己决定下一步。"
		old["system"] = "my custom system " + strategy
		if err = s.SavePrompts(old); err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.Exec("DELETE FROM settings WHERE key='model_prompts_structure_v2'"); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Prompts()
		if err != nil {
			t.Fatal(err)
		}
		if got["resume"] != prompts.LegacyDefaults()["resume"] || got["system"] != old["system"] {
			t.Fatal("incorrect migration")
		}
		var backup string
		if err = s.db.QueryRow("SELECT value FROM settings WHERE key='model_prompts_before_structure_v2'").Scan(&backup); err != nil {
			t.Fatal(err)
		}
		var original prompts.Config
		if err = json.Unmarshal([]byte(backup), &original); err != nil || !reflect.DeepEqual(original, old) {
			t.Fatal("backup does not match original", err)
		}
		// A user may deliberately restore an old template after migration.
		if err = s.SavePrompts(old); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err = s.Prompts()
		if err != nil || !reflect.DeepEqual(got, old) {
			t.Fatal("migration ran twice", err)
		}
		s.Close()
	}
}

func TestStructuredMigrationRollsBackOnFailure(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := prompts.Defaults()
	old["resume"] = "等待结束。继续看盘，并自己决定下一步。"
	if err = s.SavePrompts(old); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key='model_prompts_structure_v2'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER fail_prompt_marker BEFORE INSERT ON settings WHEN NEW.key='model_prompts_structure_v2' BEGIN SELECT RAISE(ABORT, 'test marker failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.structurePromptDefaultsV2(); err == nil {
		t.Fatal("expected migration failure")
	}
	got, err := s.Prompts()
	if err != nil || !reflect.DeepEqual(got, old) {
		t.Fatal("failed migration changed prompts", err)
	}
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM settings WHERE key IN ('model_prompts_structure_v2','model_prompts_before_structure_v2')").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial migration committed", err)
	}
}

func TestInvalidPromptUpdateIsAtomic(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before, _ := s.Prompts()
	bad := prompts.Defaults()
	bad["system"] = ""
	bad["start"] = "changed"
	if err = s.SavePrompts(bad); err == nil {
		t.Fatal("blank required prompt accepted")
	}
	after, _ := s.Prompts()
	if after["system"] != before["system"] || after["start"] != before["start"] {
		t.Fatal("partial update")
	}
}
