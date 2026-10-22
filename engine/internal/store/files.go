package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"desk/internal/agenttools"
	"desk/internal/prompts"
)

// Workspace is a virtual Markdown filesystem stored transactionally with desk data.
// Paths never reach the operating system, so files cannot escape into credentials.
type WorkspaceFile struct {
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Content   string `json:"content"`
	Revision  int    `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
}

var ErrFileConflict = errors.New("文件已存在或版本已变化，请重新读取后再保存")

func validFilePath(p string, root bool) error {
	if p == "" && root {
		return nil
	}
	if len(p) > 500 || p == "" || path.IsAbs(p) || path.Clean(p) != p || strings.ContainsAny(p, "\\:<>\"|?*\x00") {
		return errors.New("请使用工作区内的相对路径")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "." || part == ".." || strings.TrimSpace(part) != part {
			return errors.New("路径不能包含空段、上级目录或首尾空白")
		}
		for _, r := range part {
			if r < 32 {
				return errors.New("路径含控制字符")
			}
		}
	}
	if !utf8.ValidString(p) {
		return errors.New("路径必须为 UTF-8")
	}
	return nil
}

func (s *Store) WorkspaceRead(p string) (WorkspaceFile, error) {
	var f WorkspaceFile
	if err := validFilePath(p, false); err != nil {
		return f, err
	}
	err := s.db.QueryRow("SELECT path,directory,content,revision,updated_at FROM workspace_files WHERE path=?", p).Scan(&f.Path, &f.Directory, &f.Content, &f.Revision, &f.UpdatedAt)
	return f, err
}

func (s *Store) WorkspaceList(p string) ([]WorkspaceFile, error) {
	if err := validFilePath(p, true); err != nil {
		return nil, err
	}
	if p != "" {
		f, err := s.WorkspaceRead(p)
		if err != nil {
			return nil, err
		}
		if !f.Directory {
			return nil, errors.New("不是文件夹")
		}
	}
	rows, err := s.db.Query("SELECT path,directory,revision,updated_at FROM workspace_files WHERE parent=? ORDER BY directory DESC,path", p)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []WorkspaceFile{}
	for rows.Next() {
		var f WorkspaceFile
		if err := rows.Scan(&f.Path, &f.Directory, &f.Revision, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (s *Store) WorkspaceWrite(p, content string, directory bool, revision int) (WorkspaceFile, error) {
	var empty WorkspaceFile
	if err := validFilePath(p, false); err != nil {
		return empty, err
	}
	if revision < 0 || (!directory && !strings.HasSuffix(strings.ToLower(p), ".md")) || (directory && (content != "" || revision != 0)) {
		return empty, errors.New("仅支持 .md 文件；新建版本为 0，文件夹不含正文")
	}
	if strings.EqualFold(path.Base(p), prompts.InvestPath) && (p != prompts.InvestPath || revision == 0 || directory) {
		return empty, errors.New("工作指令仅允许修改已存在的根目录 INVEST.MD，不允许新建或创建副本")
	}
	if p == prompts.InvestPath && strings.TrimSpace(content) == "" {
		return empty, errors.New("INVEST.MD 是自动注入的工作指令，必须是非空 Markdown 文件")
	}
	if len(content) > 128*1024 || !utf8.ValidString(content) {
		return empty, errors.New("文件须为 UTF-8，最大 128 KiB")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	parent := path.Dir(p)
	if parent == "." {
		parent = ""
	}
	if parent != "" {
		var isDir bool
		if err := tx.QueryRow("SELECT directory FROM workspace_files WHERE path=?", parent).Scan(&isDir); err != nil || !isDir {
			return empty, errors.New("请先创建父文件夹")
		}
	}
	var count, total int
	if err := tx.QueryRow("SELECT count(*),coalesce(sum(length(CAST(content AS BLOB))),0) FROM workspace_files").Scan(&count, &total); err != nil {
		return empty, err
	}
	var oldSize int
	if revision > 0 {
		if err := tx.QueryRow("SELECT length(CAST(content AS BLOB)) FROM workspace_files WHERE path=? AND revision=? AND directory=0", p, revision).Scan(&oldSize); err != nil {
			return empty, ErrFileConflict
		}
	}
	if (revision == 0 && count >= 2000) || total-oldSize+len(content) > 32*1024*1024 {
		return empty, errors.New("工作区容量上限：2000 项、32 MiB")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var result sql.Result
	if revision == 0 {
		result, err = tx.Exec("INSERT INTO workspace_files(path,parent,directory,content,revision,updated_at) VALUES(?,?,?,?,1,?) ON CONFLICT(path) DO NOTHING", p, parent, directory, content, now)
	} else {
		result, err = tx.Exec("UPDATE workspace_files SET content=?,revision=revision+1,updated_at=? WHERE path=? AND revision=? AND directory=0", content, now, p, revision)
	}
	if err != nil {
		return empty, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return empty, err
	}
	if n != 1 {
		return empty, ErrFileConflict
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return WorkspaceFile{Path: p, Directory: directory, Content: content, Revision: revision + 1, UpdatedAt: now}, nil
}

func (s *Store) migrateWorkspace() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS workspace_files(path TEXT PRIMARY KEY,parent TEXT NOT NULL,directory INTEGER NOT NULL,content TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL,updated_at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS workspace_parent ON workspace_files(parent);`)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done string
	err = tx.QueryRow("SELECT value FROM settings WHERE key='workspace_v1'").Scan(&done)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	var raw, toolRaw string
	if err = tx.QueryRow("SELECT value FROM settings WHERE key='model_prompts'").Scan(&raw); err != nil {
		return err
	}
	if err = tx.QueryRow("SELECT value FROM settings WHERE key='agent_tools'").Scan(&toolRaw); err != nil {
		return err
	}
	var c prompts.Config
	var tc agenttools.Config
	if err = json.Unmarshal([]byte(raw), &c); err != nil {
		return err
	}
	if err = json.Unmarshal([]byte(toolRaw), &tc); err != nil {
		return err
	}
	c["system"] = prompts.WithReviews(c["system"])
	if !strings.Contains(tc.Instructions, "## Markdown 工作区工具") {
		tc.Instructions += "\n\n" + agenttools.FileInstructions
	}
	if err = prompts.ValidateStored(c); err != nil {
		return err
	}
	if err = agenttools.Validate(tc); err != nil {
		return err
	}
	cb, _ := json.Marshal(c)
	tb, _ := json.Marshal(tc)
	for key, value := range map[string]string{"model_prompts_before_workspace_v1": raw, "agent_tools_before_workspace_v1": toolRaw, "model_prompts": string(cb), "agent_tools": string(tb), "workspace_v1": "1"} {
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, f := range []WorkspaceFile{{Path: "复盘", Directory: true}, {Path: "复盘/索引.md", Content: "# 交易与判断复盘\n\n尚无已核实的复盘事件。每个事件单独创建 Markdown 文件，并在此记录事件 ID、日期、环境、币种、状态和相对文件路径。\n\n不得将历史页面上的汇总亏损擅自归因于单笔交易。\n"}} {
		parent := path.Dir(f.Path)
		if parent == "." {
			parent = ""
		}
		if _, err = tx.Exec("INSERT INTO workspace_files(path,parent,directory,content,revision,updated_at) VALUES(?,?,?,?,1,?) ON CONFLICT(path) DO NOTHING", f.Path, parent, f.Directory, f.Content, now); err != nil {
			return fmt.Errorf("初始化复盘目录: %w", err)
		}
	}
	return tx.Commit()
}
