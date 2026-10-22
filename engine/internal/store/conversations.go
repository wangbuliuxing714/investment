package store

import (
	"database/sql"
	"desk/internal/model"
	"errors"
	"time"
)

var ErrConversationChanged = errors.New("对话已切换，请刷新后重试")

func (s *Store) migrateConversations() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query("PRAGMA table_info(agent_log)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var id, notnull, pk int
		var name, typ string
		var def sql.NullString
		if err = rows.Scan(&id, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		found = found || name == "conversation_id"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !found {
		if _, err = tx.Exec("ALTER TABLE agent_log ADD COLUMN conversation_id TEXT NOT NULL DEFAULT 'trading'"); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS agent_conversations(id TEXT PRIMARY KEY,mode TEXT NOT NULL UNIQUE,created_at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS agent_log_conversation ON agent_log(conversation_id,id);
 INSERT OR IGNORE INTO agent_conversations(id,mode,created_at) VALUES('trading','trading',?);
 INSERT INTO settings(key,value) VALUES('agent_conversation','trading') ON CONFLICT(key) DO NOTHING;`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Conversation() (model.Conversation, error) {
	var c model.Conversation
	err := s.db.QueryRow("SELECT id,mode,created_at FROM agent_conversations WHERE id=(SELECT value FROM settings WHERE key='agent_conversation')").Scan(&c.ID, &c.Mode, &c.CreatedAt)
	return c, err
}

// SwitchConversation preserves each mode's transcript and returns the existing
// conversation when revisiting it. New installations keep the legacy trade log.
func (s *Store) SwitchConversation(mode, expected string) (model.Conversation, error) {
	var c model.Conversation
	if mode != "trading" && mode != "optimization" {
		return c, errors.New("代理模式必须为 trading 或 optimization")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	var active string
	if err = tx.QueryRow("SELECT value FROM settings WHERE key='agent_conversation'").Scan(&active); err != nil {
		return c, err
	}
	if expected != "" && active != expected {
		return c, ErrConversationChanged
	}
	_, err = tx.Exec("INSERT OR IGNORE INTO agent_conversations(id,mode,created_at) VALUES(?,?,?)", mode, mode, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return c, err
	}
	if err = tx.QueryRow("SELECT id,mode,created_at FROM agent_conversations WHERE mode=?", mode).Scan(&c.ID, &c.Mode, &c.CreatedAt); err != nil {
		return c, err
	}
	if _, err = tx.Exec("UPDATE settings SET value=? WHERE key='agent_conversation'", c.ID); err != nil {
		return c, err
	}
	if err = tx.Commit(); err != nil {
		return c, err
	}
	return c, nil
}

func (s *Store) AppendConversationChat(conversation, role, name, body, at string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec("INSERT INTO agent_log(conversation_id,role,name,body,created_at) VALUES(?,?,?,?,?)", conversation, role, name, body, at)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec("DELETE FROM agent_log WHERE conversation_id=? AND id NOT IN(SELECT id FROM agent_log WHERE conversation_id=? ORDER BY id DESC LIMIT ?)", conversation, conversation, ChatLogKeep)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
