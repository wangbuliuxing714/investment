package httpapi

import (
	"desk/internal/model"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStopEndpointRecordsMaintenanceAndAcceptsLegacyEmptyBody(t *testing.T) {
	for _, body := range []string{"", `{"kind":"maintenance","reason":"upgrade engine"}`} {
		s := connectionServer(t, true)
		if err := s.store.SetRunning(true); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/trader/stop", strings.NewReader(body)))
		var state model.TraderState
		err := json.Unmarshal(w.Body.Bytes(), &state)
		kind := "manual"
		if body != "" {
			kind = "maintenance"
		}
		if w.Code != 200 || err != nil || state.Running || state.StopKind != kind || state.StoppedAt == "" {
			t.Fatal(w.Code, state, err)
		}
	}
}
func TestInvalidStopReasonDoesNotChangeRunState(t *testing.T) {
	s := connectionServer(t, true)
	if err := s.store.SetRunning(true); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"kind":"error","reason":"fake"}`, `{"kind":"maintenance"}`, `{"kind":"maintenance","reason":"test","extra":true}`} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/trader/stop", strings.NewReader(body)))
		state, _ := s.store.Trader()
		if w.Code != 400 || !state.Running {
			t.Fatal(w.Code, state)
		}
	}
}
