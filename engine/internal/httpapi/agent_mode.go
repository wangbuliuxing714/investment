package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

func (s *Server) agentMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode           string `json:"mode"`
		ConversationID string `json:"conversationId"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil {
		writeErr(w, 400, err)
		return
	}
	if (body.Mode != "trading" && body.Mode != "optimization") || body.ConversationID == "" {
		writeErr(w, 400, errors.New("请提供目标模式和当前对话"))
		return
	}
	state, err := s.svc.SwitchMode(r.Context(), body.Mode, body.ConversationID)
	if err != nil {
		writeErr(w, 409, err)
		return
	}
	writeJSON(w, 200, state)
}
func (s *Server) tradingOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.svc.TradingAction(func() { next.ServeHTTP(w, r) }); err != nil {
			writeErr(w, 409, err)
		}
	})
}
