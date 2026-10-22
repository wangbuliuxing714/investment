package trader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"desk/internal/admin"
	"desk/internal/agenttools"
	"desk/internal/ai"
	"desk/internal/opencode"
	"desk/internal/prompts"
	"desk/internal/store"
)

// The model supplies structured calls; only the local executor can run them.
// No Markdown/JSON text extraction and no remote shell tool permissions exist
// on this path. The selected provider/model are retained.
type nativeAgent struct{}

func (nativeAgent) Round(ctx context.Context, in opencode.Input, messages []json.RawMessage, tools []any) (ai.Turn, error) {
	return ai.Round(ctx, in.APIURL, in.APIKey, in.Model, messages, tools)
}

type nativeHistory struct {
	frames []store.NativeFrame
	cursor int64
}

func (s *Service) nativeHistory() (nativeHistory, error) {
	frames, err := s.store.NativeFrames(s.conversationID())
	h := nativeHistory{frames: frames}
	for _, f := range frames {
		if f.SourceID > h.cursor {
			h.cursor = f.SourceID
		}
	}
	if err != nil {
		return h, err
	}
	pending, err := s.store.NativePending(s.conversationID())
	if err != nil {
		return h, err
	}
	if len(pending.Messages) > 0 {
		var request struct {
			Calls []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
		}
		if err = json.Unmarshal(pending.Messages[0], &request); err != nil {
			return h, err
		}
		completed := map[string]bool{}
		for _, raw := range pending.Messages[1:] {
			var result struct {
				ID string `json:"tool_call_id"`
			}
			if err = json.Unmarshal(raw, &result); err != nil {
				return h, err
			}
			completed[result.ID] = true
		}
		for _, call := range request.Calls {
			if !completed[call.ID] {
				pending.Messages = append(pending.Messages, ai.ToolResult(call.ID, `{"error":"execution_interrupted","executionStatus":"unknown","automaticRetry":false}`))
			}
		}
		if err = s.store.FinishNativeFrame(s.conversationID(), pending); err != nil {
			return h, err
		}
		h.frames = append(h.frames, pending)
		if pending.SourceID > h.cursor {
			h.cursor = pending.SourceID
		}
		s.say("status", "tool_interrupted", "上次工具执行被中断，未确认的调用已标为结果未知，不自动重放。")
	}
	if len(h.frames) == 0 {
		// Historical logs have no IDs. Replay them only as labelled legacy data,
		// never manufacture native tool calls/results from assistant prose.
		legacy := s.transcriptFromLog()
		for _, line := range s.Log() {
			if line.ID > h.cursor {
				h.cursor = line.ID
			}
		}
		if len(legacy) > 0 {
			err = s.nativeSave(&h, []json.RawMessage{ai.Message("user", "旧版对话日志（无原生调用ID）：\n"+transcriptText(legacy, ""))})
		}
	}
	return h, err
}
func (s *Service) nativeSave(h *nativeHistory, messages []json.RawMessage) error {
	f := store.NativeFrame{SourceID: h.cursor, Messages: messages}
	if err := s.store.AppendNativeFrame(s.conversationID(), f); err != nil {
		return fmt.Errorf("原生工具记录保存失败：%w", err)
	}
	h.frames = append(h.frames, f)
	return nil
}
func (s *Service) nativeUsers(h *nativeHistory) error {
	var messages []json.RawMessage
	for _, line := range s.Log() {
		if line.ID <= h.cursor {
			continue
		}
		if line.Role == "user" {
			messages = append(messages, ai.Message("user", line.Text))
		}
		if line.ID > h.cursor {
			h.cursor = line.ID
		}
	}
	if len(messages) > 0 {
		return s.nativeSave(h, messages)
	}
	return nil
}
func nativeMessages(h *nativeHistory, system string) []json.RawMessage {
	// Keep whole frames: a batch is never trimmed between call and results.
	start, total := len(h.frames), 0
	for start > 0 && len(h.frames)-start < 24 {
		n := 0
		for _, raw := range h.frames[start-1].Messages {
			n += len(raw)
		}
		if start < len(h.frames) && total+n > 2<<20 {
			break
		}
		start--
		total += n
	}
	msgs := []json.RawMessage{ai.Message("system", system)}
	for _, f := range h.frames[start:] {
		msgs = append(msgs, f.Messages...)
	}
	return msgs
}

func (s *Service) nativeLoop(ctx context.Context, automatic bool) error {
	h, err := s.nativeHistory()
	if err != nil {
		return err
	}
	if automatic {
		recovered, until := s.takeRecovery()
		if recovered && time.Until(until) > 0 {
			s.say("status", "engine_resume", fmt.Sprintf("引擎恢复，保留原等待计划，约 %d 秒后继续", int(time.Until(until).Seconds())))
		}
		if recovered && time.Until(until) > 0 && !s.pause(ctx, time.Until(until)) {
			return nil
		}
		event := prompts.StartEvent
		if recovered {
			event = prompts.ResumeEvent
		}
		if err = s.nativeSave(&h, []json.RawMessage{ai.Message("user", event)}); err != nil {
			return err
		}
	}
	var resolved admin.Resolved
	var modelKey string
	for ctx.Err() == nil && (automatic || !s.isRunning()) {
		st, err := s.store.Strategy()
		if err != nil {
			return err
		}
		creds := s.store.BinanceCredentials()
		if automatic && !s.optimizing() {
			if len(st.Coins) == 0 {
				return errors.New("策略里没有币种")
			}
			if creds.Key == "" || creds.Secret == "" {
				return errors.New("先在交易所页面保存币安密钥")
			}
		}
		cfg, err := s.store.Prompts()
		if err != nil {
			return err
		}
		toolsConfig, err := s.store.Tools()
		if err != nil {
			return err
		}
		invest, err := s.modeInvest()
		if err != nil {
			return err
		}
		system := s.modeSystemPrompt(st, cfg, toolsConfig, invest.Content)
		if !automatic {
			system += "\n\n" + prompts.Render(cfg["chat"], st)
		}
		settings := s.store.Settings()
		key := settings.ProviderID + "\n" + settings.UpstreamModel
		if settings.ProviderID == "" || settings.UpstreamModel == "" {
			return errors.New("先在模型页面选择模型")
		}
		if key != modelKey {
			resolved, err = s.resolveModel(ctx, settings.ProviderID, settings.UpstreamModel)
			if err != nil {
				return err
			}
			modelKey = key
		}
		s.takeNotes() // User messages are persisted before notes are queued.
		if err = s.nativeUsers(&h); err != nil {
			return err
		}
		allowed := func(name string) bool {
			if name == "wait" && !automatic {
				return false
			}
			if s.optimizing() && (!optimizationTool(name) || name == "create_directory") {
				return false
			}
			if !s.optimizing() && (creds.Key == "" || creds.Secret == "") && !store.IsFileTool(name) && name != "get_history" {
				return false
			}
			return true
		}
		s.scanning.Store(true)
		s.gate.Lock()
		turn, err := s.remote.Round(ctx, opencode.Input{ConversationID: s.conversationID(), APIURL: resolved.APIURL, APIKey: resolved.APIKey, Model: resolved.Model, Name: settings.ModelName, SystemPrompt: system}, nativeMessages(&h, system), agenttools.NativeDefinitions(allowed))
		s.gate.Unlock()
		s.scanning.Store(false)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return cloudErr(err)
		}
		if strings.TrimSpace(turn.Content) != "" {
			s.say("agent", "", turn.Content)
		}
		if len(turn.ToolCalls) == 0 {
			if err = s.nativeSave(&h, []json.RawMessage{turn.Raw}); err != nil {
				return err
			}
			if s.hasNotes() {
				continue
			}
			if automatic {
				err = s.StopWithReason("completed", "本轮已完成，代理未安排下一次运行，已停止")
				if errors.Is(err, errPendingNotes) {
					continue
				}
				return err
			}
			return nil
		}
		var client agentClient
		if !s.optimizing() && creds.Key != "" && creds.Secret != "" {
			client = s.newClient(creds.Key, creds.Secret, creds.Demo)
		}
		state := &agentState{client: client, strategy: st, service: s, paper: creds.Paper}
		messages := []json.RawMessage{turn.Raw}
		pendingSave := func() error {
			return s.store.SaveNativePending(s.conversationID(), store.NativeFrame{SourceID: h.cursor, Messages: messages})
		}
		if err = pendingSave(); err != nil {
			return fmt.Errorf("工具执行记录保存失败，本批未执行：%w", err)
		}
		var waited time.Duration
		var reason string
		for i := 0; i < len(turn.ToolCalls); {
			call := turn.ToolCalls[i]
			validation := agenttools.ValidateNative(call.Name, call.Arguments)
			if validation == nil && !allowed(call.Name) {
				validation = fmt.Errorf("tool_unavailable: %s；当前模式禁止或未配置账户", call.Name)
			}
			if validation != nil {
				raw, _ := json.Marshal(map[string]any{"error": validation.Error(), "executed": false})
				s.say("tool", call.Name, call.Arguments)
				s.say("result", call.Name, string(raw))
				messages = append(messages, ai.ToolResult(call.ID, string(raw)))
				if err = pendingSave(); err != nil {
					return err
				}
				i++
				continue
			}
			// Independent reads retain the existing worker pool. Mutations are
			// barriers, preserving account snapshots and local risk checks.
			end := i + 1
			if readOnlyTool(call.Name) {
				for end < len(turn.ToolCalls) {
					next := turn.ToolCalls[end]
					if !readOnlyTool(next.Name) || !allowed(next.Name) || agenttools.ValidateNative(next.Name, next.Arguments) != nil {
						break
					}
					end++
				}
			}
			calls := make([]toolCall, 0, end-i)
			for _, c := range turn.ToolCalls[i:end] {
				calls = append(calls, toolCall{c.Name, c.Arguments})
			}
			results, d, r := s.runToolCalls(ctx, state, calls, automatic)
			if d > 0 {
				waited, reason = d, r
			}
			for j, c := range turn.ToolCalls[i:end] {
				text := `{"error":"execution_cancelled","executed":false}`
				if j < len(results) {
					text = results[j].Text
				}
				messages = append(messages, ai.ToolResult(c.ID, text))
			}
			if err = pendingSave(); err != nil {
				return err
			}
			i = end
		}
		frame := store.NativeFrame{SourceID: h.cursor, Messages: messages}
		if err = s.store.FinishNativeFrame(s.conversationID(), frame); err != nil {
			return err
		}
		h.frames = append(h.frames, frame)
		if ctx.Err() != nil {
			return nil
		}
		if waited > 0 {
			s.say("status", "", "代理安排 "+waitLabel(waited, reason)+"后继续")
			if !s.pause(ctx, waited) {
				return nil
			}
			if !s.hasNotes() {
				if err = s.nativeSave(&h, []json.RawMessage{ai.Message("user", prompts.Render(cfg["resume"], st))}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
