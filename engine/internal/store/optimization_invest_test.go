package store

import (
	"desk/internal/agenttools"
	"desk/internal/model"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptimizationInvestPreservesEditsAcrossMigrationAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	f, err := s.ModeInvest("optimization")
	if err != nil || f.Path != "INVEST.MD" || !strings.Contains(f.Content, "## 策略优化模式") {
		t.Fatal(f, err)
	}
	before, err := s.Invest()
	if err != nil || before != f {
		t.Fatal(err)
	}
	f, err = s.WorkspaceWrite(f.Path, "用户维护的优化规则", false, f.Revision)
	if err != nil {
		t.Fatal(err)
	}
	// An existing document takes precedence even on the first migration.
	if _, err = s.db.Exec("DELETE FROM settings WHERE key='optimization_invest_v1'"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateOptimizationInvest(); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveTools(agenttools.Config{Instructions: "用户工具说明，无自动附加"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.ModeInvest("optimization")
	if err != nil || after != f {
		t.Fatal("optimization edits overwritten", after, err)
	}
	trading, err := s.ModeInvest("trading")
	if err != nil || trading != f {
		t.Fatal("modes did not share root instructions", trading, err)
	}
	tools, err := s.Tools()
	if err != nil || agenttools.Render(tools, model.Strategy{}) != "用户工具说明，无自动附加" {
		t.Fatal(tools, err)
	}
	for _, text := range []string{"", " \n\t"} {
		if _, err = s.WorkspaceWrite(f.Path, text, false, f.Revision); err == nil {
			t.Fatal("blank instructions accepted")
		}
	}
	// A corrupted/missing document fails closed; there is no fallback template.
	if _, err = s.db.Exec("DELETE FROM workspace_files WHERE path=?", f.Path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ModeInvest("optimization"); err == nil {
		t.Fatal("missing instructions accepted")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ModeInvest("optimization"); err == nil {
		t.Fatal("restart silently reseeded deleted instructions")
	}
}

func TestOptimizationInvestMigrationRollsBack(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.Exec("DELETE FROM settings WHERE key='optimization_invest_v1'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM workspace_files WHERE path IN ('优化','优化/INVEST.MD')"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER fail_optimizer_init BEFORE INSERT ON settings WHEN NEW.key='optimization_invest_v1' BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateOptimizationInvest(); err == nil {
		t.Fatal("expected failure")
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM workspace_files WHERE path IN ('优化','优化/INVEST.MD')").Scan(&count); err != nil || count != 0 {
		t.Fatal("partial migration", count, err)
	}
}
