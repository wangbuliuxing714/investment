package binance

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var errorURL = regexp.MustCompile(`https?://[^\s"<>]+`)
var errorCredential = regexp.MustCompile(`(?i)(signature|api[-_]?key|secret)=([^\s&"<>]+)`)

// Errors are sent to the UI and model. Signed request URLs must never appear.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "restricted location") || strings.Contains(lower, "eligibility") {
		return errors.New("币安拒绝当前网络出口（地区限制）。请核对账户适用地区及可用网络配置；这不是密钥错误。")
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		msg = ue.Err.Error()
	}
	lower = strings.ToLower(msg)
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return errors.New("币安连接超时，未能取得最新数据。请检查网络及代理连接。")
	}
	if strings.Contains(lower, "forcibly closed") || strings.Contains(lower, "connection reset") || strings.Contains(lower, "wsarecv") {
		return errors.New("币安连接被中断，未能取得最新数据。请检查代理模式、节点及网络连接。")
	}
	if strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused") || strings.Contains(lower, "proxyconnect") {
		return errors.New("无法建立币安连接，请检查本机代理是否运行及端口配置。")
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("币安请求已取消")
	}
	msg = errorURL.ReplaceAllString(msg, "[请求地址已省略]")
	msg = errorCredential.ReplaceAllString(msg, "$1=[已隐藏]")
	if strings.Contains(msg, "API-key") || strings.Contains(msg, "Signature") {
		return fmt.Errorf("币安密钥或签名校验失败：%s", msg)
	}
	runes := []rune(msg)
	if len(runes) > 400 {
		msg = string(runes[:400]) + "…"
	}
	return fmt.Errorf("币安：%s", msg)
}
