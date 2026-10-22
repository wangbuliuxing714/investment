package store

import (
	"database/sql"
	"desk/internal/agenttools"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) migrateOptimizationInvest() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var marker string
	err = tx.QueryRow("SELECT value FROM settings WHERE key='optimization_invest_v1'").Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, f := range []WorkspaceFile{{Path: "优化", Directory: true}} {
		var directory bool
		var content string
		err = tx.QueryRow("SELECT directory,content FROM workspace_files WHERE path=?", f.Path).Scan(&directory, &content)
		if err == nil {
			if directory != f.Directory || (!directory && strings.TrimSpace(content) == "") {
				return fmt.Errorf("优化工作指令初始化冲突：%s", f.Path)
			}
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		var count, total int
		if err = tx.QueryRow("SELECT count(*),coalesce(sum(length(CAST(content AS BLOB))),0) FROM workspace_files").Scan(&count, &total); err != nil {
			return err
		}
		if count >= 2000 || total+len(f.Content) > 32*1024*1024 {
			return errors.New("初始化优化工作指令超出工作区容量限制")
		}
		parent := ""
		if !f.Directory {
			parent = "优化"
		}
		if _, err = tx.Exec("INSERT INTO workspace_files(path,parent,directory,content,revision,updated_at) VALUES(?,?,?,?,1,?)", f.Path, parent, f.Directory, f.Content, now); err != nil {
			return err
		}
	}
	// Preserve the previously implicit history catalog in editable settings once.
	// Subsequent requests use only saved text; deleted guidance stays deleted.
	var raw string
	if err = tx.QueryRow("SELECT value FROM settings WHERE key='agent_tools'").Scan(&raw); err != nil {
		return err
	}
	var tools agenttools.Config
	if err = json.Unmarshal([]byte(raw), &tools); err != nil {
		return err
	}
	if !strings.Contains(tools.Instructions, "## 本地历史行情库") {
		tools.Instructions += "\n\n" + agenttools.HistoryInstructions
		if err = agenttools.Validate(tools); err != nil {
			return err
		}
		body, err := json.Marshal(tools)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('agent_tools_before_explicit_history_v1',?) ON CONFLICT(key) DO NOTHING", raw); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE settings SET value=? WHERE key='agent_tools'", string(body)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('optimization_invest_v1','1')"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ModeInvest(mode string) (WorkspaceFile, error) {
	if mode != "optimization" && mode != "trading" {
		return WorkspaceFile{}, errors.New("无效代理模式")
	}
	return s.Invest()
}
