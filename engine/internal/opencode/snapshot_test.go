package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSnapshotRequestsNeverReuseRemoteAssistantHistory(t *testing.T) {
	created := 0
	var generated []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/session" {
			created++
			fmt.Fprintf(w, `{"data":{"id":"snapshot-%d"}}`, created)
			return
		}
		generated = append(generated, r.URL.Path)
		fmt.Fprint(w, `{"text":"ok"}`)
	}))
	defer srv.Close()
	c := &Client{Local: strings.TrimPrefix(srv.URL, "http://"), pass: "test", http: srv.Client(), sessionID: "contaminated-session"}
	for i := 0; i < 2; i++ {
		if text, err := c.reply(context.Background(), "model", "authoritative local snapshot"); err != nil || text != "ok" {
			t.Fatal(text, err)
		}
		if c.sessionID != "" {
			t.Fatal("remote context retained")
		}
	}
	if created != 2 || len(generated) != 2 || generated[0] == generated[1] || strings.Contains(strings.Join(generated, " "), "contaminated") {
		t.Fatal("remote context reused", created, generated)
	}
}
