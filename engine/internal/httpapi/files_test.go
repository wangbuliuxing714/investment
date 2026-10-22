package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFilesAPIRevisionAndValidation(t *testing.T) {
	s := connectionServer(t, true)
	for _, tc := range []struct {
		method, url, body string
		status            int
	}{
		{"GET", "/api/files?path=", "", 200},
		{"GET", "/api/files?path=missing.md&read=1", "", 404},
		{"POST", "/api/files", `{"name":"write_file","arguments":{"path":"note.md","content":"first","revision":0}}`, 200},
		{"POST", "/api/files", `{"name":"write_file","arguments":{"path":"note.md","content":"second","revision":1}}`, 200},
		{"POST", "/api/files", `{"name":"write_file","arguments":{"path":"note.md","content":"stale","revision":1}}`, 409},
		{"POST", "/api/files", `{"name":"write_file","arguments":{"path":"../note.md","content":"x","revision":0}}`, 400},
		{"POST", "/api/files", `{"name":"submit_decisions","arguments":{}}`, 400},
	} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.url, strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.url, w.Code, w.Body.String())
		}
	}
	state, _ := s.svc.State()
	if state.Running || state.Busy {
		t.Fatal("file action started trading")
	}
}
