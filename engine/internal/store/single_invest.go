package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"desk/internal/prompts"
)

// Merge existing documents only once, backing up their complete original
// records in the same transaction. Runtime requests never append defaults.
func (s *Store) migrateSingleInvest() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var marker string
	err = tx.QueryRow("SELECT value FROM settings WHERE key='single_invest_v1'").Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	var root, legacy WorkspaceFile
	if err = tx.QueryRow("SELECT path,directory,content,revision,updated_at FROM workspace_files WHERE path=?", prompts.InvestPath).Scan(&root.Path, &root.Directory, &root.Content, &root.Revision, &root.UpdatedAt); err != nil {
		return err
	}
	if root.Directory || strings.TrimSpace(root.Content) == "" {
		return errors.New("根目录 INVEST.MD 无效，无法合并")
	}
	optimization := prompts.OptimizationDefault
	err = tx.QueryRow("SELECT path,directory,content,revision,updated_at FROM workspace_files WHERE path=?", prompts.OptimizationInvestPath).Scan(&legacy.Path, &legacy.Directory, &legacy.Content, &legacy.Revision, &legacy.UpdatedAt)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		if legacy.Directory {
			return errors.New("旧优化工作指令是目录，无法安全合并")
		}
		optimization = legacy.Content
	}
	// Remove only obsolete path/ownership statements; retain custom research
	// rules and record the originals verbatim in the migration backup.
	optimization = strings.ReplaceAll(optimization, "优化/INVEST.MD", "INVEST.MD")
	optimization = strings.ReplaceAll(optimization, "根目录 INVEST.MD 属于交易模式，不作为当前模式的工作指令。", "")
	content := strings.NewReplacer("{{tradingInstructions}}", demoteHeadings(root.Content), "{{optimizationInstructions}}", demoteHeadings(optimization)).Replace(prompts.WorkspaceDefault)
	var total int
	if err = tx.QueryRow("SELECT coalesce(sum(length(CAST(content AS BLOB))),0) FROM workspace_files").Scan(&total); err != nil {
		return err
	}
	if len(content) > 128*1024 || total-len(root.Content)-len(legacy.Content)+len(content) > 32*1024*1024 {
		return errors.New("合并工作指令超出工作区容量限制")
	}
	backup, err := json.Marshal([]WorkspaceFile{root, legacy})
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('invest_before_single_v1',?)", string(backup)); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE workspace_files SET content=?,revision=revision+1,updated_at=? WHERE path=?", content, time.Now().UTC().Format(time.RFC3339Nano), prompts.InvestPath); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM workspace_files WHERE path=?", prompts.OptimizationInvestPath); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('single_invest_v1','1')"); err != nil {
		return err
	}
	return tx.Commit()
}

func demoteHeadings(content string) string {
	lines := strings.Split(content, "\n")
	fence := ""
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			if fence == "" {
				fence = trim[:3]
			} else if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if fence == "" && strings.HasPrefix(line, "#") {
			n := len(line) - len(strings.TrimLeft(line, "#"))
			if n < len(line) && line[n] == ' ' {
				lines[i] = strings.Repeat("#", min(6, n+2)) + line[n:]
			}
		}
	}
	return strings.Join(lines, "\n")
}
