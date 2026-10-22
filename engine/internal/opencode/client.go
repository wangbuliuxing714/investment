package opencode

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ErrUnavailable means the desk OpenCode process or its tunnel cannot be used.
// A bad model reply is not unavailable: the scan must stop instead of asking again.
var ErrUnavailable = errors.New("交易 OpenCode 连不上")

type downError struct{ msg string }

func (e *downError) Error() string { return e.msg }
func (e *downError) Unwrap() error { return ErrUnavailable }

func down(msg string) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = ErrUnavailable.Error()
	}
	return &downError{msg: msg}
}

// Client talks to the desk OpenCode process through an SSH local forward.
// Orders are never sent from here. The password stays in memory.
type Client struct {
	Host       string
	User       string
	KeyPath    string
	Passphrase string
	Local      string
	Remote     string
	Directory  string
	ConfigPath string
	Unit       string

	opMu            sync.Mutex
	mu              sync.Mutex
	ssh             *ssh.Client
	sshDone         chan struct{}
	ln              net.Listener
	pass            string
	http            *http.Client
	sessionID       string
	sessionModel    string
	conversationKey string
}

func (c *Client) Ask(ctx context.Context, in Input) (string, error) {
	if !enabled() {
		return "", down("本机没有启用服务器交易 OpenCode")
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return "", errors.New("没有要问的内容")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.prepareLocked(ctx, in); err != nil {
		return "", err
	}
	return c.reply(ctx, in.Model, in.Prompt)
}

// Prepare writes the selected model into the server process once.
func (c *Client) Prepare(ctx context.Context, in Input) error {
	if !enabled() {
		return down("本机没有启用服务器交易 OpenCode")
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.prepareLocked(ctx, in)
}

// Say asks the current server session. Prepare must have succeeded for this model.
func (c *Client) Say(ctx context.Context, modelName, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("没有要问的内容")
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.ensure(ctx); err != nil {
		return "", err
	}
	if c.sessionModel != modelName {
		c.sessionID = ""
		c.sessionModel = modelName
	}
	return c.reply(ctx, modelName, prompt)
}

func (c *Client) prepareLocked(ctx context.Context, in Input) error {
	c.selectConversation(in.ConversationID)
	if err := c.ensure(ctx); err != nil {
		return err
	}
	restarted, err := c.sync(ctx, in)
	if err != nil {
		return err
	}
	if restarted || c.sessionModel != in.Model {
		c.sessionID = ""
		c.sessionModel = in.Model
	}
	return nil
}

func (c *Client) selectConversation(key string) {
	if c.conversationKey != key {
		c.sessionID = ""
		c.conversationKey = key
	}
}

func (c *Client) sync(ctx context.Context, in Input) (bool, error) {
	body, err := BuildConfig(in)
	if err != nil {
		return false, err
	}
	want := hashBytes(body)
	got, err := c.remoteHash(ctx)
	if err != nil {
		return false, err
	}
	if got == want {
		return false, c.waitHealthy(ctx)
	}
	if err := c.putConfig(ctx, body); err != nil {
		return false, err
	}
	got, err = c.remoteHash(ctx)
	if err != nil {
		return false, err
	}
	if got != want {
		return false, errors.New("交易 OpenCode 的模型配置没有写上")
	}
	if err := c.restart(ctx); err != nil {
		return true, err
	}
	c.sessionID = ""
	return true, c.waitHealthy(ctx)
}

func (c *Client) remoteHash(ctx context.Context) (string, error) {
	out, err := c.python(ctx, "hash", nil)
	if err != nil {
		return "", err
	}
	sum := strings.TrimSpace(string(out))
	if len(sum) != 64 {
		return "", errors.New("交易 OpenCode 的配置没有读到")
	}
	return sum, nil
}

func (c *Client) putConfig(ctx context.Context, body []byte) error {
	out, err := c.python(ctx, "put", body)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return errors.New("交易 OpenCode 的模型配置没有写上")
	}
	_, err = c.run(ctx, "chown opencode-v2:opencode-v2 "+shellQuote(c.ConfigPath)+" && chmod 600 "+shellQuote(c.ConfigPath), nil)
	if err != nil {
		return errors.New("交易 OpenCode 的模型配置没有写上")
	}
	return nil
}

func (c *Client) restart(ctx context.Context) error {
	// Only the desk unit. The novel worker and its gateway stay as they are.
	if _, err := c.run(ctx, "systemctl restart "+c.Unit, nil); err != nil {
		return down("交易 OpenCode 没有重新启动")
	}
	out, err := c.run(ctx, "systemctl is-active "+c.Unit, nil)
	if err != nil || strings.TrimSpace(string(out)) != "active" {
		return down("交易 OpenCode 没有重新启动")
	}
	return nil
}

func (c *Client) python(ctx context.Context, op string, stdin []byte) ([]byte, error) {
	script := remoteScript
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	cmd := "python3 -c 'import base64; exec(base64.b64decode(\"" + encoded + "\").decode())' " + shellQuote(op) + " " + shellQuote(c.ConfigPath)
	return c.run(ctx, cmd, stdin)
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

const remoteScript = `import hashlib, os, pathlib, sys
path = pathlib.Path(sys.argv[2])
op = sys.argv[1]
if op == "hash":
    data = path.read_bytes() if path.exists() else b""
    sys.stdout.write(hashlib.sha256(data).hexdigest())
elif op == "put":
    import shutil
    data = sys.stdin.buffer.read()
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".json.tmp")
    tmp.write_bytes(data)
    os.chmod(tmp, 0o600)
    shutil.chown(tmp, "opencode-v2", "opencode-v2")
    os.replace(tmp, path)
    os.chmod(path, 0o600)
    shutil.chown(path, "opencode-v2", "opencode-v2")
    sys.stdout.write("ok")
elif op == "password":
    env = pathlib.Path("/etc/opencode-desk.env")
    for line in env.read_text(encoding="utf-8").splitlines():
        if line.startswith("OPENCODE_PASSWORD="):
            sys.stdout.write(line.split("=", 1)[1].strip())
            raise SystemExit
    raise SystemExit(1)
elif op == "status":
    import json, subprocess
    def active(unit):
        p = subprocess.run(["systemctl", "is-active", unit], capture_output=True, text=True)
        return (p.stdout or "").strip()
    def pid(port):
        p = subprocess.run(["ss", "-ltnp"], capture_output=True, text=True)
        needle = ":" + str(port)
        for line in p.stdout.splitlines():
            at = line.find(needle)
            if at < 0:
                continue
            end = at + len(needle)
            if end < len(line) and line[end].isdigit():
                continue
            mark = line.find("pid=")
            if mark < 0:
                continue
            num = ""
            for ch in line[mark + 4:]:
                if ch.isdigit():
                    num += ch
                else:
                    break
            if num:
                return int(num)
        return 0
    sys.stdout.write(json.dumps({
        "desk": active("opencode-desk"),
        "novel": active("opencode-v2"),
        "gateway": active("opencode-v2-gateway"),
        "p8790": pid(8790),
        "p8793": pid(8793),
    }))
else:
    raise SystemExit(2)
`

type Status struct {
	Desk    string `json:"desk"`
	Novel   string `json:"novel"`
	Gateway string `json:"gateway"`
	P8790   int    `json:"p8790"`
	P8793   int    `json:"p8793"`
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.ensure(ctx); err != nil {
		return Status{}, err
	}
	out, err := c.python(ctx, "status", nil)
	if err != nil {
		return Status{}, err
	}
	var st Status
	if json.Unmarshal(bytes.TrimSpace(out), &st) != nil {
		return Status{}, errors.New("没有读到服务器进程状态")
	}
	return st, nil
}

func (c *Client) password(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.pass != "" {
		pass := c.pass
		c.mu.Unlock()
		return pass, nil
	}
	c.mu.Unlock()
	out, err := c.python(ctx, "password", nil)
	if err != nil {
		return "", down("读不到交易 OpenCode 的密码")
	}
	pass := strings.TrimSpace(string(out))
	if pass == "" || strings.ContainsAny(pass, "\r\n") {
		return "", down("读不到交易 OpenCode 的密码")
	}
	c.mu.Lock()
	c.pass = pass
	c.mu.Unlock()
	return pass, nil
}

func (c *Client) refreshPassword(ctx context.Context) (string, error) {
	c.mu.Lock()
	c.pass = ""
	c.mu.Unlock()
	return c.password(ctx)
}

func (c *Client) httpClient() *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.http == nil {
		c.http = &http.Client{Timeout: 0}
	}
	return c.http
}

func (c *Client) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(20 * time.Second)
	var last error
	for {
		err := c.health(ctx)
		if err == nil {
			return nil
		}
		last = err
		if !errors.Is(err, ErrUnavailable) {
			return err
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return last
		}
		timer := time.NewTimer(400 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return down("交易 OpenCode 没有就绪")
		case <-timer.C:
		}
	}
}

func (c *Client) health(ctx context.Context) error {
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := c.request(call, http.MethodGet, "/api/health", nil, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return down("交易 OpenCode 没有就绪")
	}
	return nil
}

var errNoSession = errors.New("session missing")

func (c *Client) reply(ctx context.Context, modelName, prompt string) (string, error) {
	// The caller supplies a complete, bounded transcript from the local audit
	// log on every request. Reusing remote history duplicates that transcript
	// and retains assistant-authored results which the local parser rejected.
	// Local conversation IDs and workspace files remain the durable history.
	c.sessionID = ""
	defer func() { c.sessionID = "" }()
	if err := c.ensureSession(ctx, modelName); err != nil {
		return "", err
	}
	text, err := c.complete(ctx, c.sessionID, prompt)
	if errors.Is(err, errNoSession) {
		c.sessionID = ""
		if err = c.ensureSession(ctx, modelName); err != nil {
			return "", err
		}
		return c.complete(ctx, c.sessionID, prompt)
	}
	return text, err
}

func (c *Client) ensureSession(ctx context.Context, modelName string) error {
	if c.sessionID != "" {
		return nil
	}
	created, err := c.request(ctx, http.MethodPost, "/api/session", sessionPayload(modelName, c.Directory), true)
	if err != nil {
		return err
	}
	body, err := readBody(created)
	if err != nil {
		return err
	}
	if created.StatusCode < 200 || created.StatusCode >= 300 {
		return fmt.Errorf("交易 OpenCode 没有建会话（%d）", created.StatusCode)
	}
	id, err := sessionID(body)
	if err != nil {
		return err
	}
	c.sessionID = id
	c.sessionModel = modelName
	return nil
}

func (c *Client) complete(ctx context.Context, id, prompt string) (string, error) {
	genCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := c.request(genCtx, http.MethodPost, "/api/session/"+url.PathEscape(id)+"/generate", map[string]string{"prompt": prompt}, true)
	if err != nil {
		return "", err
	}
	payload, err := readBody(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", errNoSession
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("交易 OpenCode 没有生成回复")
	}
	text, err := generatedText(payload)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("交易 OpenCode 没有生成回复")
	}
	return text, nil
}

func sessionPayload(modelID, directory string) map[string]any {
	return map[string]any{
		"title":    "desk-scan",
		"agent":    "desk",
		"model":    map[string]string{"id": modelID, "providerID": providerID},
		"location": map[string]string{"directory": directory},
	}
}

func (c *Client) request(ctx context.Context, method, path string, payload any, hardAuth bool) (*http.Response, error) {
	resp, err := c.do(ctx, method, path, payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if _, err := c.refreshPassword(ctx); err != nil {
		return nil, err
	}
	resp, err = c.do(ctx, method, path, payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if hardAuth {
			return nil, errors.New("交易 OpenCode 拒绝了登录")
		}
		return nil, down("交易 OpenCode 拒绝了登录")
	}
	return resp, nil
}

func (c *Client) do(ctx context.Context, method, path string, payload any) (*http.Response, error) {
	pass, err := c.password(ctx)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.Local+path, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", pass)
	req.Header.Set("x-opencode-directory", c.Directory)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, down("交易 OpenCode 没有连上")
	}
	return resp, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, down("交易 OpenCode 的回复中断了")
	}
	return body, nil
}

func sessionID(body []byte) (string, error) {
	var wrap struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &wrap) != nil {
		return "", errors.New("交易 OpenCode 没有返回会话")
	}
	id := wrap.Data.ID
	if id == "" {
		id = wrap.ID
	}
	if id == "" || strings.Contains(id, "/") {
		return "", errors.New("交易 OpenCode 没有返回会话")
	}
	return id, nil
}

func generatedText(body []byte) (string, error) {
	var wrap struct {
		Data struct {
			Text string `json:"text"`
			Data struct {
				Text string `json:"text"`
			} `json:"data"`
		} `json:"data"`
		Text string `json:"text"`
	}
	if json.Unmarshal(body, &wrap) != nil {
		return "", errors.New("交易 OpenCode 没有生成回复")
	}
	if wrap.Data.Text != "" {
		return wrap.Data.Text, nil
	}
	if wrap.Data.Data.Text != "" {
		return wrap.Data.Data.Text, nil
	}
	if wrap.Text != "" {
		return wrap.Text, nil
	}
	return "", errors.New("交易 OpenCode 没有生成回复")
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
