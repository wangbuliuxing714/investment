package store

import (
	"database/sql"
	"desk/internal/agenttools"
	"desk/internal/prompts"
	"encoding/json"
	"time"
)

type NativeFrame struct {
	SourceID int64
	Messages []json.RawMessage
}

func (s *Store) migrateNative() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS agent_native_frames (
 id INTEGER PRIMARY KEY AUTOINCREMENT,conversation_id TEXT NOT NULL,source_id INTEGER NOT NULL,
 messages TEXT NOT NULL,created_at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS native_conversation ON agent_native_frames(conversation_id,id);`)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec(`CREATE TABLE IF NOT EXISTS agent_native_pending(conversation_id TEXT PRIMARY KEY,source_id INTEGER NOT NULL,messages TEXT NOT NULL);`); err != nil {
		return err
	}
	return s.migrateNativeProtocol()
}

func (s *Store) migrateNativeProtocol() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var marker string
	err = tx.QueryRow("SELECT value FROM settings WHERE key='native_tools_v1'").Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	for _, key := range []string{"model_prompts", "agent_tools"} {
		var raw string
		if err = tx.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&raw); err != nil {
			return err
		}
		var c map[string]string
		if err = json.Unmarshal([]byte(raw), &c); err != nil {
			return err
		}
		changed := false
		for k, v := range c {
			next := agenttools.NativeProtocolText(v)
			if next != v {
				c[k] = next
				changed = true
			}
		}
		if !changed {
			continue
		}
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO NOTHING", key+"_before_native_v1", raw); err != nil {
			return err
		}
		body, e := json.Marshal(c)
		if e != nil {
			return e
		}
		if _, err = tx.Exec("UPDATE settings SET value=? WHERE key=?", string(body), key); err != nil {
			return err
		}
	}
	var f WorkspaceFile
	if err = tx.QueryRow("SELECT path,directory,content,revision,updated_at FROM workspace_files WHERE path=?", prompts.InvestPath).Scan(&f.Path, &f.Directory, &f.Content, &f.Revision, &f.UpdatedAt); err != nil {
		return err
	}
	if next := agenttools.NativeProtocolText(f.Content); next != f.Content {
		backup, e := json.Marshal(f)
		if e != nil {
			return e
		}
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('invest_before_native_v1',?) ON CONFLICT(key) DO NOTHING", string(backup)); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE workspace_files SET content=?,revision=revision+1,updated_at=? WHERE path=?", next, time.Now().UTC().Format(time.RFC3339Nano), f.Path); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('native_tools_v1','1')"); err != nil {
		return err
	}
	return tx.Commit()
}

// An assistant call batch and all of its results are one durable frame. A
// restart never replays an incomplete call or executes an old mutation again.
func (s *Store) AppendNativeFrame(conversation string, frame NativeFrame) error {
	return s.appendNativeFrame(conversation, frame, false)
}

func (s *Store) FinishNativeFrame(conversation string, frame NativeFrame) error {
	return s.appendNativeFrame(conversation, frame, true)
}

func (s *Store) appendNativeFrame(conversation string, frame NativeFrame, finish bool) error {
	raw, err := json.Marshal(frame.Messages)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO agent_native_frames(conversation_id,source_id,messages,created_at) VALUES(?,?,?,?)", conversation, frame.SourceID, string(raw), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM agent_native_frames WHERE conversation_id=? AND id NOT IN (SELECT id FROM agent_native_frames WHERE conversation_id=? ORDER BY id DESC LIMIT 100)", conversation, conversation); err != nil {
		return err
	}
	if finish {
		if _, err = tx.Exec("DELETE FROM agent_native_pending WHERE conversation_id=?", conversation); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Commit the request before any side effect, and completed results as they
// arrive. If the engine dies, unmatched calls have unknown execution status;
// recovery reports them instead of silently repeating an order or file write.
func (s *Store) SaveNativePending(conversation string, frame NativeFrame) error {
	raw, err := json.Marshal(frame.Messages)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO agent_native_pending(conversation_id,source_id,messages) VALUES(?,?,?) ON CONFLICT(conversation_id) DO UPDATE SET source_id=excluded.source_id,messages=excluded.messages", conversation, frame.SourceID, string(raw))
	return err
}
func (s *Store) NativePending(conversation string) (NativeFrame, error) {
	var f NativeFrame
	var raw string
	err := s.db.QueryRow("SELECT source_id,messages FROM agent_native_pending WHERE conversation_id=?", conversation).Scan(&f.SourceID, &raw)
	if err == sql.ErrNoRows {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	err = json.Unmarshal([]byte(raw), &f.Messages)
	return f, err
}

func (s *Store) NativeFrames(conversation string) ([]NativeFrame, error) {
	rows, err := s.db.Query("SELECT source_id,messages FROM agent_native_frames WHERE conversation_id=? ORDER BY id DESC LIMIT 24", conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	frames := []NativeFrame{}
	for rows.Next() {
		var f NativeFrame
		var raw string
		if err = rows.Scan(&f.SourceID, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &f.Messages); err != nil {
			return nil, err
		}
		frames = append(frames, f)
	}
	for i, j := 0, len(frames)-1; i < j; i, j = i+1, j-1 {
		frames[i], frames[j] = frames[j], frames[i]
	}
	return frames, rows.Err()
}
