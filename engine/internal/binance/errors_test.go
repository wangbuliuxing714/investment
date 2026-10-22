package binance

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestConnectionErrorsAreActionableAndNeverContainSignedURLs(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&url.Error{Op: "Get", URL: "https://demo-fapi.binance.com/private?signature=secret-value", Err: errors.New("wsarecv: An existing connection was forcibly closed by the remote host")}, "连接被中断"},
		{&url.Error{Op: "Get", URL: "https://example.com/?signature=secret-value", Err: context.DeadlineExceeded}, "连接超时"},
		{errors.New("Service unavailable from a restricted location according to b. Eligibility"), "地区限制"},
		{errors.New("Get https://example.com/?signature=secret-value unexpected signature=secret-value"), "请求地址已省略"},
	} {
		got := wrap(tc.err).Error()
		if !strings.Contains(got, tc.want) || strings.Contains(got, "secret-value") || strings.Contains(got, "https://") {
			t.Fatalf("unsafe or misleading error: %s", got)
		}
	}
	if wrap(nil) != nil {
		t.Fatal("nil must remain nil")
	}
}
