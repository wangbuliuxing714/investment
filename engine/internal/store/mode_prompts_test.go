package store

import (
	"desk/internal/prompts"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestModePromptMigrationPreservesStrategyAndRecordsAndOnlyRunsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	old := prompts.InvestDefaults()
	old["system"] = "自定义身份"
	old["chat"] = "自定义面板要求"
	old["resume"] = "自定义恢复事件"
	raw, _ := json.Marshal(old)
	if _, err = s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('model_prompts_before_modes_v1','invest_before_modes_v1')"); err != nil {
		t.Fatal(err)
	}
	f, _ := s.Invest()
	original := "# 策略和做事规则\n\n旧说明\n\n## 文件怎么改\n文件修改要求；不建版本文件，改策略就改本文件\n\n## 两种模式\n旧模式说明\n\n## 正在交易用的策略\nSMA50 > SMA200\n\n### 原有过程评分继续保留\n仅交易评分\n\n## 研究草案\n当前研究版本 v4\n\n```md\n## 文件怎么改\n字面策略示例\n```\n\n### 版本和每次问题怎么记\n按版本逐条保留问题\n"
	f, err = s.WorkspaceWrite(f.Path, original, false, f.Revision)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.WorkspaceWrite("复盘.md", "# v4\n\nv4-05：原始问题和当时判断", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.migrateModePrompts(); err != nil {
		t.Fatal(err)
	}
	c, err := s.Prompts()
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.Invest()
	for _, text := range []string{"SMA50 > SMA200", "当前研究版本 v4", "```md\n## 文件怎么改\n字面策略示例\n```"} {
		if !strings.Contains(after.Content, text) {
			t.Fatal("strategy changed", text)
		}
	}
	for _, text := range []string{"文件修改要求", "旧模式说明", "仅交易评分", "按版本逐条保留问题"} {
		if strings.Contains(after.Content, text) {
			t.Fatal("instruction left in strategy", text)
		}
	}
	if !strings.Contains(c["system"], "自定义身份") || !strings.Contains(c["system"], "文件修改要求") || !strings.Contains(c["system"], "按版本逐条保留问题") || !strings.Contains(c["trading"], "仅交易评分") || strings.Contains(c["optimization"], "仅交易评分") || c["chat"] != old["chat"] || c["resume"] != old["resume"] {
		t.Fatal("saved rules lost or mixed", c)
	}
	if !strings.Contains(c["system"], "不建版本文件，改策略就改 INVEST.MD") {
		t.Fatal("moved file references corrupted")
	}
	var backup WorkspaceFile
	if s.setting("model_prompts_before_modes_v1") == "" || json.Unmarshal([]byte(s.setting("invest_before_modes_v1")), &backup) != nil || backup.Content != f.Content {
		t.Fatal("original backup missing")
	}
	// Custom saved content, including deleted instructions, must stay authoritative.
	c["optimization"] = "用户修改后的优化要求"
	if err = s.SavePrompts(c); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := s.Prompts()
	againFile, _ := s.Invest()
	againReview, _ := s.WorkspaceRead(review.Path)
	if !reflect.DeepEqual(c, again) || againFile != after || againReview != review {
		t.Fatal("reopen changed saved content")
	}
}

func TestModePromptMigrationRollsBackTogether(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	raw, _ := json.Marshal(prompts.InvestDefaults())
	s.db.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(raw))
	s.db.Exec("DELETE FROM settings WHERE key IN ('model_prompts_before_modes_v1','invest_before_modes_v1')")
	f, _ := s.Invest()
	f, err = s.WorkspaceWrite(f.Path, "# 策略和做事规则\n\n## 文件怎么改\n文件要求\n\n## 策略\n买卖条件", false, f.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("CREATE TRIGGER fail_modes BEFORE UPDATE ON workspace_files BEGIN SELECT RAISE(ABORT,'failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateModePrompts(); err == nil {
		t.Fatal("expected failure")
	}
	after, _ := s.Invest()
	if after != f || s.setting("model_prompts") != string(raw) || s.setting("model_prompts_before_modes_v1") != "" || s.setting("invest_before_modes_v1") != "" {
		t.Fatal("partial migration")
	}
}
