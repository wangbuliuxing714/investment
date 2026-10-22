package httpapi

import (
	"desk/internal/model"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModeAPISeparatesTranscriptAndBlocksManualOrders(t *testing.T) {
	s := connectionServer(t, true)
	switchMode := func(body string, code int) {
		t.Helper()
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/agent/mode", strings.NewReader(body)))
		if w.Code != code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	switchMode(`{"mode":"unknown","conversationId":"trading"}`, 400)
	switchMode(`{"mode":"optimization","conversationId":"trading"}`, 200)
	state, _ := s.svc.State()
	if state.Mode != "optimization" || state.Running || state.ConversationID == "trading" {
		t.Fatal(state)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/agent/log?conversationId=trading", nil))
	if w.Code != 409 {
		t.Fatal("stale log accepted")
	}
	for _, path := range []string{"/api/positions/close", "/api/positions/protection"} {
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if w.Code != 409 || !strings.Contains(w.Body.String(), "策略优化") {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	switchMode(`{"mode":"trading","conversationId":"trading"}`, 409)
	switchMode(`{"mode":"trading","conversationId":"optimization"}`, 200)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/trader", nil))
	var result model.TraderState
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Mode != "trading" || result.Running {
		t.Fatal(result)
	}
}
