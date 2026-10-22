package opencode

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSSHKeyReadErrorsDistinguishMissingAndPermissions(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{{os.ErrNotExist, "文件不存在"}, {os.ErrPermission, "无权读取"}, {errors.New("disk failure"), "读取失败"}} {
		err := keyReadError(&os.PathError{Op: "open", Path: "private-key", Err: tc.err})
		if !strings.Contains(err.Error(), tc.want) || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("wrong key diagnosis: %v", err)
		}
	}
}
