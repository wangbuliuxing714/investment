package store

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func IsFileTool(name string) bool {
	switch name {
	case "list_files", "read_file", "write_file", "create_directory":
		return true
	}
	return false
}

func (s *Store) FileCommand(name, args string) (any, error) {
	var p struct {
		Path     string  `json:"path"`
		Content  *string `json:"content"`
		Revision *int    `json:"revision"`
	}
	decoder := json.NewDecoder(strings.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("只能提交一个参数对象")
	}
	if name != "write_file" && (p.Content != nil || p.Revision != nil) {
		return nil, errors.New("此工具不接受正文或版本")
	}
	switch name {
	case "list_files":
		return s.WorkspaceList(p.Path)
	case "read_file":
		f, err := s.WorkspaceRead(p.Path)
		if err != nil {
			return nil, err
		}
		if f.Directory {
			return nil, errors.New("这是文件夹，请列出子项")
		}
		return f, nil
	case "create_directory":
		return s.WorkspaceWrite(p.Path, "", true, 0)
	case "write_file":
		if p.Content == nil || p.Revision == nil {
			return nil, errors.New("content 和 revision 必填")
		}
		return s.WorkspaceWrite(p.Path, *p.Content, false, *p.Revision)
	default:
		return nil, errors.New("未知文件工具")
	}
}
