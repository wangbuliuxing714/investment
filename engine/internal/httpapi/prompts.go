package httpapi

import (
	"desk/internal/prompts"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func (s *Server) getPrompts(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.Prompts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writePrompts(w, c)
}

func (s *Server) writePrompts(w http.ResponseWriter, c prompts.Config) {
	st, err := s.store.Strategy()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	invest, err := s.store.Invest()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"prompts": c, "definitions": prompts.Definitions, "variables": prompts.Variables,
		"parameters": prompts.Parameters(st),
		"presets":    prompts.Presets,
		"invest":     invest,
		"preview":    map[string]string{"system": prompts.Render(c["system"], st), "instructions": prompts.Render(invest.Content, st), "chat": prompts.Render(c["chat"], st), "trading": prompts.Render(c["trading"], st), "optimization": prompts.Render(c["optimization"], st)},
	})
}

func (s *Server) savePrompts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompts prompts.Config `json:"prompts"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeErr(w, http.StatusBadRequest, errors.New("请只提交一份提示词配置"))
		return
	}
	if err := prompts.Validate(body.Prompts); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SavePrompts(body.Prompts); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writePrompts(w, body.Prompts)
}
