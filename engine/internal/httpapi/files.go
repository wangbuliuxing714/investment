package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"desk/internal/store"
)

func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	name := "list_files"
	var args []byte
	if r.Method == http.MethodGet {
		if r.URL.Query().Get("read") == "1" {
			name = "read_file"
		}
		args, _ = json.Marshal(map[string]string{"path": r.URL.Query().Get("path")})
	} else {
		var command struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		d.DisallowUnknownFields()
		if err := d.Decode(&command); err != nil {
			writeErr(w, 400, err)
			return
		}
		if d.Decode(new(any)) != io.EOF {
			writeErr(w, 400, errors.New("只能提交一个对象"))
			return
		}
		name, args = command.Name, command.Arguments
	}
	result, err := s.store.FileCommand(name, string(args))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrFileConflict) {
			status = http.StatusConflict
		}
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("文件或文件夹不存在")
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
