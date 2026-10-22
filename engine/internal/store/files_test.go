package store

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWorkspacePersistenceAndConflictingWriters(t *testing.T) {
	db := filepath.Join(t.TempDir(), "desk.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WorkspaceWrite("复盘/月", "", true, 0); err != nil {
		t.Fatal(err)
	}
	f, err := s.WorkspaceWrite("复盘/月/事件.md", "原判断", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, content := range []string{"修订甲", "修订乙"} {
		wg.Add(1)
		go func(c string) { defer wg.Done(); _, e := s.WorkspaceWrite(f.Path, c, false, f.Revision); results <- e }(content)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrFileConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("lost-update protection failed: %d %d", success, conflict)
	}
	c, _ := s.Prompts()
	c["system"] += "\n用户补充规则"
	if err = s.SavePrompts(c); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	saved, err := s.WorkspaceRead(f.Path)
	if err != nil || saved.Revision != 2 || saved.Content == "原判断" {
		t.Fatalf("readback: %+v %v", saved, err)
	}
	cfg, _ := s.Prompts()
	if cfg["system"] != c["system"] {
		t.Fatal("restart changed user prompt")
	}
	invest, err := s.Invest()
	if err != nil || strings.Contains(invest.Content, "## 文件记忆与逐次复盘责任") || strings.Count(cfg["trading"], "## 文件记忆与逐次复盘责任") != 1 {
		t.Fatal("duplicate review policy")
	}
}

func TestWorkspaceRejectsInvalidPathsAndWrites(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, p := range []string{"../secret.md", "/root.md", "C:/secret.md", "a/../b.md", "a\\b.md", "x.txt", "x.md:stream", "a//b.md", "复盘/../../b.md"} {
		if _, err = s.WorkspaceWrite(p, "x", false, 0); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
	if _, err = s.WorkspaceWrite("missing/note.md", "x", false, 0); err == nil {
		t.Fatal("missing parent accepted")
	}
	if _, err = s.WorkspaceWrite("huge.md", strings.Repeat("x", 128*1024+1), false, 0); err == nil {
		t.Fatal("oversize accepted")
	}
	if _, err = s.FileCommand("write_file", `{"path":"note.md","content":"x"}`); err == nil {
		t.Fatal("missing revision accepted")
	}
	if _, err = s.FileCommand("write_file", `{"path":"note.md","content":"x","revision":0}`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WorkspaceWrite("note.md", "overwrite", false, 0); !errors.Is(err, ErrFileConflict) {
		t.Fatal("creation overwrote file")
	}
	if _, err = s.WorkspaceWrite("note.md/child", "", true, 0); err == nil {
		t.Fatal("file used as parent")
	}
	rows, err := s.WorkspaceList("")
	if err != nil || len(rows) != 4 {
		t.Fatalf("list: %+v %v", rows, err)
	}
}
