package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"desk/internal/prompts"
)

func TestSingleInvestMergesLegacyRulesBacksUpAndSurvivesRestart(t *testing.T) {
	db := filepath.Join(t.TempDir(), "desk.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	root, _ := s.Invest()
	root, err = s.WorkspaceWrite(root.Path, "# 我的交易规则\n\n交易自定义规则\n\n```md\n# literal heading\n```", false, root.Revision)
	if err != nil {
		t.Fatal(err)
	}
	legacy := "# 优化规则\n\n自定义优化规则\n优化/INVEST.MD"
	if _, err = s.db.Exec("INSERT INTO workspace_files(path,parent,directory,content,revision,updated_at) VALUES(?,'优化',0,?,7,'original-time')", prompts.OptimizationInvestPath, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('single_invest_v1','invest_before_single_v1')"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateSingleInvest(); err != nil {
		t.Fatal(err)
	}
	f, err := s.Invest()
	if err != nil || f.Revision != root.Revision+1 || !strings.Contains(f.Content, "交易自定义规则") || !strings.Contains(f.Content, "自定义优化规则") || strings.Contains(f.Content, "优化/INVEST.MD") || !strings.Contains(f.Content, "```md\n# literal heading\n```") {
		t.Fatal("migration lost rules or altered code", err)
	}
	if _, err = s.WorkspaceRead(prompts.OptimizationInvestPath); err == nil {
		t.Fatal("duplicate instructions remain")
	}
	var backups []WorkspaceFile
	if err = json.Unmarshal([]byte(s.setting("invest_before_single_v1")), &backups); err != nil || len(backups) != 2 || backups[0] != root || backups[1].Content != legacy || backups[1].Revision != 7 {
		t.Fatal("original backup changed", err)
	}
	if err = s.migrateSingleInvest(); err != nil {
		t.Fatal(err)
	}
	f2, _ := s.Invest()
	if f2 != f {
		t.Fatal("migration not idempotent")
	}
	f, err = s.WorkspaceWrite(f.Path, "用户后续编辑，不重新追加默认指令", false, f.Revision)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"trading", "optimization"} {
		actual, e := s.ModeInvest(mode)
		if e != nil || actual != f {
			t.Fatal(mode, actual, e)
		}
	}
	for _, p := range []string{"INVEST.MD", "invest.md", "Invest.Md", "优化/INVEST.MD", "优化/invest.md"} {
		if _, err = s.WorkspaceWrite(p, "副本", false, 0); err == nil {
			t.Fatal("created duplicate", p)
		}
		if _, err = s.WorkspaceWrite(p, "", true, 0); err == nil {
			t.Fatal("created reserved directory", p)
		}
	}
}

func TestSingleInvestMigrationFailureLeavesBothOriginals(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before, _ := s.Invest()
	if _, err = s.db.Exec("INSERT INTO workspace_files(path,parent,directory,content,revision,updated_at) VALUES(?,'优化',0,'legacy',3,'original')", prompts.OptimizationInvestPath); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('single_invest_v1','invest_before_single_v1')"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER fail_single_invest BEFORE INSERT ON settings WHEN NEW.key='single_invest_v1' BEGIN SELECT RAISE(ABORT,'failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateSingleInvest(); err == nil {
		t.Fatal("expected failure")
	}
	after, _ := s.Invest()
	legacy, err := s.WorkspaceRead(prompts.OptimizationInvestPath)
	if after != before || err != nil || legacy.Content != "legacy" || s.setting("invest_before_single_v1") != "" {
		t.Fatal("partial migration", err)
	}
}
