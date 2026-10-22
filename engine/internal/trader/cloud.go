package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"desk/internal/agenttools"
	"desk/internal/binance"
	"desk/internal/model"
	"desk/internal/prompts"
	"desk/internal/store"
)

const (
	minWait = 15 * time.Second
	maxWait = 15 * time.Minute
)

type memory struct {
	Role string
	Text string
}

func (s *Service) cloudLoop(ctx context.Context) error { return s.nativeLoop(ctx, true) }

func (s *Service) pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-s.wake:
			if s.hasNotes() {
				return true
			}
		case <-timer.C:
			return true
		}
	}
}

func (s *Service) hasNotes() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.notes) > 0
}

func (s *Service) replyOnce() {
	defer s.scanning.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	s.mu.Lock()
	if s.switching.Load() {
		s.mu.Unlock()
		cancel()
		s.replying.Store(false)
		return
	}
	s.replyCancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.replyCancel = nil
		s.replying.Store(false)
		more := len(s.notes) > 0 && s.cancel == nil && !s.switching.Load()
		s.mu.Unlock()
		if more && s.replying.CompareAndSwap(false, true) {
			go s.replyOnce()
		}
	}()
	for {
		if s.isRunning() || ctx.Err() != nil {
			return
		}
		notes := s.takeNotes()
		if len(notes) == 0 {
			return
		}
		if err := s.answer(ctx); err != nil && ctx.Err() == nil {
			s.say("status", "", err.Error())
			return
		}
	}
}

func (s *Service) answer(ctx context.Context) error { return s.nativeLoop(ctx, false) }

func (s *Service) transcriptFromLog() []memory {
	lines := s.Log()
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	var mem []memory
	for _, line := range lines {
		switch line.Role {
		case "user":
			mem = append(mem, memory{Role: "用户", Text: line.Text})
		case "agent":
			// Older logs may contain model-authored tool-result transcripts.
			// They are not engine results, including when replaying an old session.
			text, _, diagnostic := parseToolReply(line.Text)
			if text != "" {
				mem = append(mem, memory{Role: "代理", Text: text})
			}
			if diagnostic != "" {
				mem = append(mem, memory{Role: "引擎协议校验", Text: diagnostic})
			}
		case "status":
			if line.Name == "tool_protocol" {
				mem = append(mem, memory{Role: "引擎协议校验", Text: line.Text})
			}
		case "tool":
			mem = append(mem, memory{Role: "工具调用 " + toolLabel(line.Name), Text: clipForModel(line.Text)})
		case "result":
			text := line.Text
			// Candles are chronological. Keeping only the first 6000 bytes drops
			// recent candles and later symbols when a conversation is replayed.
			if line.Name != "get_market" && line.Name != "get_history" && !binance.IsAnalyticsTool(line.Name) && !store.IsFileTool(line.Name) {
				text = clipForModel(text)
			}
			mem = append(mem, memory{Role: "工具 " + toolLabel(line.Name), Text: text})
		}
	}
	return mem
}

func cloudErr(err error) error {
	return fmt.Errorf("模型原生工具接口请求失败：%w", err)
}

func systemPrompt(st model.Strategy, cfg prompts.Config, toolsConfig agenttools.Config, invest string) string {
	return selectedSystemPrompt(st, cfg, toolsConfig, invest, "trading")
}

func selectedSystemPrompt(st model.Strategy, cfg prompts.Config, toolsConfig agenttools.Config, invest, mode string) string {
	baseline, _ := json.Marshal(st)
	return prompts.Instructions(cfg, st) + "\n\n" + prompts.Render(cfg[mode], st) +
		"\n\n当前代理模式：" + mode + "\n\n## 策略：INVEST.MD\n" + prompts.Render(invest, st) +
		"\n\n## 当前策略 JSON\n" + string(baseline) + "\n\n## 工具上下文\n" + agenttools.Render(toolsConfig, st)
}

func cloudPrompt(st model.Strategy, cfg prompts.Config, toolsConfig agenttools.Config, mem []memory) string {
	return transcriptText(mem, "")
}

func chatPrompt(st model.Strategy, cfg prompts.Config, toolsConfig agenttools.Config, mem []memory) string {
	return prompts.Render(cfg["chat"], st) + "\n\n" + transcriptText(mem, "")
}

func clipForModel(text string) string {
	const limit = 6000
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "\n…（面板里保留了完整结果）"
}

func transcriptText(mem []memory, lead string) string {
	if len(mem) > 8 {
		mem = mem[len(mem)-8:]
	}
	var b strings.Builder
	b.WriteString(lead)
	for _, item := range mem {
		fmt.Fprintf(&b, "\n\n[%s]\n%s", item.Role, item.Text)
	}
	return b.String()
}

func waitDuration(args string) (time.Duration, string) {
	var body struct {
		Seconds int    `json:"seconds"`
		Reason  string `json:"reason"`
	}
	_ = json.Unmarshal([]byte(args), &body)
	d := time.Duration(body.Seconds) * time.Second
	if d < minWait {
		d = minWait
	}
	if d > maxWait {
		d = maxWait
	}
	return d, strings.TrimSpace(body.Reason)
}

func waitLabel(d time.Duration, reason string) string {
	text := fmt.Sprintf("%d 秒", int(d.Seconds()))
	if reason != "" {
		text += "（" + reason + "）"
	}
	return text
}

func shortArgs(args string) string {
	args = strings.TrimSpace(args)
	if args == "" || args == "{}" {
		return ""
	}
	if len(args) > 4000 {
		cut := 4000
		for cut > 0 && !utf8.RuneStart(args[cut]) {
			cut--
		}
		return args[:cut] + "…"
	}
	return args
}

type toolCall struct {
	Name string
	Args string
}

func parseToolCalls(raw string) (string, []toolCall) {
	prose, calls, _ := parseToolReply(raw)
	return prose, calls
}

// Result roles belong to the engine. Stop at an assistant-authored result
// boundary, retaining only requests before it. Scan whole JSON values and code
// blocks so role markers in file contents and quoted examples remain data.
func parseToolReply(raw string) (string, []toolCall, string) {
	diagnostic := ""
	for pos := 0; pos < len(raw); {
		lineEnd := len(raw)
		if n := strings.IndexByte(raw[pos:], '\n'); n >= 0 {
			lineEnd = pos + n + 1
		}
		line := strings.TrimSpace(raw[pos:lineEnd])
		if assistantResultBoundary(line) {
			raw = raw[:pos]
			diagnostic = `{"error":"assistant_tool_result_rejected","executed":false,"detail":"模型回复包含自行生成的工具结果；该段及其后的请求未执行，也未作为真实结果写入上下文。此前的工具请求以引擎实际返回为准。"}`
			break
		}
		if strings.HasPrefix(line, "{") || jsonArrayStart(line) {
			decoder := json.NewDecoder(strings.NewReader(raw[pos:]))
			var value json.RawMessage
			if decoder.Decode(&value) != nil {
				break
			}
			pos += int(decoder.InputOffset())
			continue
		}
		if strings.HasPrefix(line, "```") || line == "<tool_call>" {
			close := "```"
			if line == "<tool_call>" {
				close = "</tool_call>"
			}
			pos = lineEnd
			for pos < len(raw) {
				end := len(raw)
				if n := strings.IndexByte(raw[pos:], '\n'); n >= 0 {
					end = pos + n + 1
				}
				closed := strings.TrimSpace(raw[pos:end]) == close
				pos = end
				if closed {
					break
				}
			}
			continue
		}
		pos = lineEnd
	}
	// XML markers are structural only on their own line; never rewrite literal
	// tags inside a JSON string being saved to a research document.
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case "<tool_call>":
			lines[i] = "```tool"
		case "</tool_call>":
			lines[i] = "```"
		}
	}
	raw = strings.Join(lines, "\n")
	var prose strings.Builder
	var calls []toolCall
	rest := raw
	for {
		i := toolFence(rest)
		if i < 0 {
			plain, bare := parseBareToolCalls(rest)
			prose.WriteString(plain)
			calls = append(calls, bare...)
			break
		}
		plain, bare := parseBareToolCalls(rest[:i])
		prose.WriteString(plain)
		calls = append(calls, bare...)
		rest = rest[i+3:]
		lang := ""
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			lang = strings.TrimSpace(rest[:nl])
			rest = rest[nl+1:]
		}
		body := rest
		if end := toolFence(rest); end >= 0 {
			body = rest[:end]
			rest = rest[end+3:]
		} else {
			rest = ""
		}
		call, ok := decodeTool(body)
		if ok && knownTool(call.Name) && (lang == "tool" || lang == "json" || lang == "") {
			calls = append(calls, call)
			continue
		}
		prose.WriteString("```")
		if lang != "" {
			prose.WriteString(lang)
			prose.WriteByte('\n')
		}
		prose.WriteString(body)
		if body != "" && !strings.HasSuffix(body, "```") && strings.Contains(raw, "```") {
			prose.WriteString("```")
		}
	}
	return strings.TrimSpace(prose.String()), calls, diagnostic
}

func assistantResultBoundary(line string) bool {
	return line == "[本轮工具执行结果]" || line == "[工具执行结果]" ||
		line == "[工具结果]" || line == "[result]" || line == "[tool]" ||
		line == "[工具]" || (strings.HasPrefix(line, "[工具 ") && strings.HasSuffix(line, "]"))
}

func jsonArrayStart(line string) bool {
	if !strings.HasPrefix(line, "[") {
		return false
	}
	// Human-readable transcript labels are not JSON arrays. Malformed arrays
	// still fail closed rather than exposing nested envelopes as calls.
	rest := strings.TrimSpace(line[1:])
	return rest == "" || strings.ContainsAny(rest[:1], `{[\"-0123456789]`) ||
		strings.HasPrefix(rest, "true") || strings.HasPrefix(rest, "false") || strings.HasPrefix(rest, "null")
}

// Markdown fences start on their own line; literal backticks inside JSON file
// content are data and must not split an otherwise valid tool envelope.
func toolFence(raw string) int {
	for pos := 0; pos < len(raw); {
		n := strings.Index(raw[pos:], "```")
		if n < 0 {
			return -1
		}
		i := pos + n
		lineStart := strings.LastIndexByte(raw[:i], '\n') + 1
		if strings.TrimSpace(raw[lineStart:i]) == "" {
			return i
		}
		pos = i + 3
	}
	return -1
}

// Accept standalone JSON tool envelopes when a model omits Markdown fences.
// Decode whole JSON values so nested examples, quoted braces, and newlines in
// file content cannot be mistaken for additional calls. Bare trade mutations
// still require the explicit fenced protocol used before this compatibility fix.
func parseBareToolCalls(raw string) (string, []toolCall) {
	var prose strings.Builder
	var calls []toolCall
	for pos := 0; pos < len(raw); {
		lineEnd := len(raw)
		if n := strings.IndexByte(raw[pos:], '\n'); n >= 0 {
			lineEnd = pos + n + 1
		}
		line := raw[pos:lineEnd]
		trimmed := strings.TrimLeft(line, " \t\r")
		if !strings.HasPrefix(trimmed, "{") && !jsonArrayStart(strings.TrimSpace(trimmed)) {
			prose.WriteString(line)
			pos = lineEnd
			continue
		}
		start := pos + len(line) - len(trimmed)
		decoder := json.NewDecoder(strings.NewReader(raw[start:]))
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			// Do not search inside a malformed JSON block for executable fragments.
			prose.WriteString(raw[pos:])
			break
		}
		end := start + int(decoder.InputOffset())
		next := len(raw)
		if n := strings.IndexByte(raw[end:], '\n'); n >= 0 {
			next = end + n + 1
		}
		call, ok := decodeTool(string(value))
		bareAllowed := readOnlyTool(call.Name) || store.IsFileTool(call.Name) || call.Name == "wait"
		if ok && bareAllowed && strings.TrimSpace(raw[end:next]) == "" {
			calls = append(calls, call)
		} else {
			prose.WriteString(raw[pos:next])
		}
		pos = next
	}
	return prose.String(), calls
}

func decodeTool(body string) (toolCall, bool) {
	var wrap struct {
		Name      string          `json:"name"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(body)), &wrap) != nil {
		return toolCall{}, false
	}
	name := strings.TrimSpace(wrap.Name)
	if name == "" {
		name = strings.TrimSpace(wrap.Tool)
	}
	if name == "" {
		return toolCall{}, false
	}
	args := strings.TrimSpace(string(wrap.Arguments))
	if args == "" || args == "null" {
		args = "{}"
	}
	if !strings.HasPrefix(args, "{") {
		return toolCall{}, false
	}
	return toolCall{Name: name, Args: args}, true
}

func knownTool(name string) bool {
	if store.IsFileTool(name) {
		return true
	}
	if readOnlyTool(name) {
		return true
	}
	switch name {
	case "get_account", "get_positions", "get_market", "submit_decisions", "set_protection", "wait":
		return true
	default:
		return false
	}
}
