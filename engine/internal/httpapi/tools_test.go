package httpapi

import (
	"desk/internal/agenttools"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestToolsAPIIsIndependentAndNeverExecutesTools(t *testing.T) {
	s := connectionServer(t, true)
	before, _ := s.store.Prompts()
	update := agenttools.Config{Instructions: "independent {{coins}} tools"}
	body, _ := json.Marshal(update)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings/tools", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var view struct {
		Config      agenttools.Config
		Definitions []agenttools.Definition
		Preview     string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Config != update || len(view.Definitions) != 17 || strings.Contains(view.Preview, "{{coins}}") {
		t.Fatal("missing tool config or rendered preview")
	}
	after, _ := s.store.Prompts()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("tools save overwrote prompts")
	}
	for _, bad := range []string{`{}`, `null`, `{"instructions":""}`, `{"instructions":"ok","enabled":true}`, string(body) + `{}`} {
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings/tools", strings.NewReader(bad)))
		if w.Code != http.StatusBadRequest {
			t.Fatal("invalid tools accepted", bad, w.Code)
		}
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings/tools", nil))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Config != update {
		t.Fatal("invalid save changed tools", err)
	}
	state, _ := s.svc.State()
	if state.Running || state.Busy {
		t.Fatal("saving tool configuration started agent")
	}
	if len(s.svc.Log()) != 0 {
		t.Fatal("saving tools triggered execution")
	}
}
