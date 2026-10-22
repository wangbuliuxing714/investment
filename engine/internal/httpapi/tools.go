package httpapi

import (
	"desk/internal/agenttools"
	"desk/internal/prompts"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func (s *Server) getTools(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.Tools()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writeTools(w, c)
}

func (s *Server) writeTools(w http.ResponseWriter, c agenttools.Config) {
	st, err := s.store.Strategy()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config": c, "defaults": agenttools.Defaults(), "definitions": agenttools.Definitions,
		"variables": prompts.Variables, "parameters": prompts.Parameters(st), "preview": agenttools.Render(c, st),
	})
}

func (s *Server) saveTools(w http.ResponseWriter, r *http.Request) {
	var c agenttools.Config
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeErr(w, http.StatusBadRequest, errors.New("请只提交一份工具配置"))
		return
	}
	if err := agenttools.Validate(c); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SaveTools(c); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writeTools(w, c)
}
