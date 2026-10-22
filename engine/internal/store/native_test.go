package store

import (
	"desk/internal/agenttools"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeProtocolMigrationPreservesUserContentAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	f, _ := s.Invest()
	original := "# 研究版本 v4\n\n参数阈值：0.75；按原定成本测。\n\n先用简短中文说明，再用 tool JSON 代码块调用：`{\"name\":\"工具名\",\"arguments\":{}}`。独立查询可一起发，需要上次结果的就按顺序。\n\n保留原始问题和原因。"
	f, err = s.WorkspaceWrite(f.Path, original, false, f.Revision)
	if err != nil {
		t.Fatal(err)
	}
	tools := agenttools.Config{Instructions: agenttools.LegacyInstructions + "\nCUSTOM_KEEP"}
	if err = s.SaveTools(tools); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DELETE FROM settings WHERE key IN ('native_tools_v1','invest_before_native_v1','agent_tools_before_native_v1')"); err != nil {
		t.Fatal(err)
	}
	if err = s.migrateNativeProtocol(); err != nil {
		t.Fatal(err)
	}
	next, _ := s.Invest()
	got, _ := s.Tools()
	if next.Content != agenttools.NativeProtocolText(original) || next.Revision != f.Revision+1 || !strings.Contains(got.Instructions, "CUSTOM_KEEP") || strings.Contains(got.Instructions, "```tool") {
		t.Fatal("lost content or obsolete protocol")
	}
	var backup WorkspaceFile
	if json.Unmarshal([]byte(s.setting("invest_before_native_v1")), &backup) != nil || backup != f {
		t.Fatal("missing complete backup")
	}
	// A later deliberate edit is authoritative; migrations must not reinject.
	next, err = s.WorkspaceWrite(next.Path, "用户自行编辑", false, next.Revision)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.Invest()
	if after != next {
		t.Fatal("migration ran again")
	}
}
