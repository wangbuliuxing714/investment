package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"desk/internal/prompts"
)

func TestInvestMigrationPreservesContentAndUserEditsAcrossReopen(t *testing.T) {
	db := filepath.Join(t.TempDir(), "desk.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	old := prompts.UnifiedDefaults()
	old["system"] = "## 角色定义\n自定义角色\n\n## 行为规则\n原样保留 {{coins}}\n\n### 细则\n中文与空行\n"
	old["start"] = "我的启动要求"
	old["noTool"] = "我的无工具要求"
	raw, _ := json.Marshal(old)
	f, _ := s.Invest()
	if _, err = s.WorkspaceWrite(f.Path, "已有用户文档", false, f.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('model_prompts_before_invest_v1','invest_before_migration_v1')"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateInvestPrompt(); err != nil {
		t.Fatal(err)
	}
	var cfg prompts.Config
	err = json.Unmarshal([]byte(s.setting("model_prompts")), &cfg)
	if err != nil || len(cfg) != 3 || cfg["system"] != "## 角色定义\n自定义角色" {
		t.Fatalf("role not separated: %+v %v", cfg, err)
	}
	f, err = s.Invest()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"已有用户文档", "## 行为规则\n原样保留 {{coins}}\n\n### 细则\n中文与空行", old["start"], old["noTool"]} {
		if !strings.Contains(f.Content, want) {
			t.Fatalf("lost original text: %s", want)
		}
	}
	if strings.Contains(f.Content, "自定义角色") {
		t.Fatal("role duplicated in INVEST.MD")
	}
	if s.setting("model_prompts_before_invest_v1") != string(raw) || s.setting("invest_before_migration_v1") != "已有用户文档" {
		t.Fatal("original backup missing")
	}
	if err = s.migrateModePrompts(); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Prompts()
	if err != nil {
		t.Fatal(err)
	}
	f, err = s.Invest()
	if err != nil {
		t.Fatal(err)
	}
	f, err = s.WorkspaceWrite(f.Path, "用户后续编辑", false, f.Revision)
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := s.Tools()
	s.Close()
	s, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.Invest()
	againCfg, _ := s.Prompts()
	againTools, _ := s.Tools()
	if again != f || !reflect.DeepEqual(againCfg, cfg) || againTools != tools {
		t.Fatal("reopening rewrote migrated content")
	}
	for _, c := range []string{"", " \n\t"} {
		if _, err = s.WorkspaceWrite(f.Path, c, false, f.Revision); err == nil {
			t.Fatal("blank instructions accepted")
		}
	}
	if _, err = s.WorkspaceWrite(f.Path, "", true, 0); err == nil {
		t.Fatal("instruction directory accepted")
	}
}

func TestInvestMigrationRollsBackFileConfigAndBackups(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := prompts.UnifiedDefaults()
	raw, _ := json.Marshal(old)
	if _, err = s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('model_prompts_before_invest_v1','invest_before_migration_v1','agent_tools_before_invest_v1')"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Invest()
	if _, err = s.db.Exec("CREATE TRIGGER fail_invest BEFORE UPDATE ON settings WHEN OLD.key='agent_tools' BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateInvestPrompt(); err == nil {
		t.Fatal("expected migration failure")
	}
	after, _ := s.Invest()
	if before != after || s.setting("model_prompts") != string(raw) {
		t.Fatal("partial migration committed")
	}
	var count int
	if err = s.db.QueryRow("SELECT COUNT(*) FROM settings WHERE key IN ('model_prompts_before_invest_v1','invest_before_migration_v1','agent_tools_before_invest_v1')").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial backup committed", err)
	}
}
