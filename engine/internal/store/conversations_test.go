package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestConversationMigrationPreservesLegacyAndIsolatesRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desk.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE agent_log(id INTEGER PRIMARY KEY AUTOINCREMENT,role TEXT NOT NULL,name TEXT NOT NULL DEFAULT '',body TEXT NOT NULL,created_at TEXT NOT NULL);INSERT INTO agent_log(role,body,created_at) VALUES('user','legacy trade','before');`)
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	trade, e := s.RecentChat(10)
	if e != nil || len(trade) != 1 || trade[0].Text != "legacy trade" {
		t.Fatal(trade, e)
	}
	c, e := s.SwitchConversation("optimization", "trading")
	if e != nil || c.Mode != "optimization" {
		t.Fatal(c, e)
	}
	empty, _ := s.RecentChat(10)
	if len(empty) != 0 {
		t.Fatal("old context leaked")
	}
	for i := 0; i < ChatLogKeep+2; i++ {
		if _, e = s.AppendChat("user", "", fmt.Sprint(i), "now"); e != nil {
			t.Fatal(e)
		}
	}
	optimization, _ := s.RecentChat(ChatLogKeep)
	if len(optimization) != ChatLogKeep {
		t.Fatal(len(optimization))
	}
	saved, _ := s.ConversationChat("trading", 10)
	if len(saved) != 1 || saved[0].Text != "legacy trade" {
		t.Fatal("optimization trimmed trade records")
	}
	if _, e = s.SwitchConversation("trading", "trading"); e != ErrConversationChanged {
		t.Fatal("stale client switched", e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	active, _ := s.Conversation()
	if active.ID != c.ID {
		t.Fatal("mode not persisted")
	}
	if _, e = s.SwitchConversation("trading", c.ID); e != nil {
		t.Fatal(e)
	}
	saved, _ = s.RecentChat(10)
	if len(saved) != 1 || saved[0].ID != trade[0].ID {
		t.Fatal(saved)
	}
	if _, e = s.SwitchConversation("optimization", "trading"); e != nil {
		t.Fatal(e)
	}
	saved, _ = s.RecentChat(ChatLogKeep)
	if len(saved) != ChatLogKeep {
		t.Fatal("optimization conversation lost")
	}
}
