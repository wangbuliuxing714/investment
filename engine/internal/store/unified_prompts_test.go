package store

import (
	"desk/internal/prompts"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestUnifiedMigrationPreservesTextBackupsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	old := prompts.LegacyDefaults()
	old["system"] = "user system"
	old["strategy"] = "  user strategy with {{coins}}  "
	old["history"] = "custom history"
	old["wait"] = "retired custom instruction"
	raw, _ := json.Marshal(old)
	toolBefore, _ := s.Tools()
	toolBefore.Instructions = "original tools"
	if err = s.SaveTools(toolBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('model_prompts_before_unified_v3','agent_tools_before_unified_v3')"); err != nil {
		t.Fatal(err)
	}
	if err = s.unifySystemPrompts(); err != nil {
		t.Fatal(err)
	}
	expected, protocol := prompts.Consolidate(old)
	var unifiedRaw string
	err = s.db.QueryRow("SELECT value FROM settings WHERE key='model_prompts'").Scan(&unifiedRaw)
	var got prompts.Config
	if err == nil {
		err = json.Unmarshal([]byte(unifiedRaw), &got)
	}
	if err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatal("consolidation mismatch", err)
	}
	if !strings.Contains(got["system"], old["strategy"]) || strings.Contains(got["system"], "retired custom instruction") {
		t.Fatal("custom strategy or retired setting mishandled")
	}
	tools, _ := s.Tools()
	if tools.Instructions != toolBefore.Instructions+"\n\n"+protocol {
		t.Fatal("protocol did not move intact")
	}
	var original string
	if err = s.db.QueryRow("SELECT value FROM settings WHERE key='model_prompts_before_unified_v3'").Scan(&original); err != nil || original != string(raw) {
		t.Fatal("prompt backup missing", err)
	}
	if err = s.db.QueryRow("SELECT value FROM settings WHERE key='agent_tools_before_unified_v3'").Scan(&original); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(original, "original tools") {
		t.Fatal("tool backup missing")
	}
	if err = s.migrateInvestPrompt(); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateModePrompts(); err != nil {
		t.Fatal(err)
	}
	expected, err = s.Prompts()
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.Prompts()
	againTools, _ := s.Tools()
	if !reflect.DeepEqual(again, expected) || againTools != tools {
		t.Fatal("migration ran twice")
	}
}

func TestUnifiedMigrationFailureIsAtomic(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := prompts.LegacyDefaults()
	raw, _ := json.Marshal(old)
	beforeTools, _ := s.Tools()
	if _, err = s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('model_prompts_before_unified_v3','agent_tools_before_unified_v3')"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER fail_system_unify BEFORE UPDATE ON settings WHEN OLD.key='agent_tools' BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.unifySystemPrompts(); err == nil {
		t.Fatal("expected failure")
	}
	var after string
	s.db.QueryRow("SELECT value FROM settings WHERE key='model_prompts'").Scan(&after)
	if after != string(raw) {
		t.Fatal("partial migration")
	}
	tools, _ := s.Tools()
	if tools != beforeTools {
		t.Fatal("tools changed on failure")
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM settings WHERE key IN ('model_prompts_before_unified_v3','agent_tools_before_unified_v3')").Scan(&count)
	if count != 0 {
		t.Fatal("partial backup")
	}
}
