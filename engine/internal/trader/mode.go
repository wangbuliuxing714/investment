package trader

import (
	"context"
	"desk/internal/agenttools"
	"desk/internal/model"
	"desk/internal/prompts"
	"desk/internal/store"
	"encoding/json"
	"errors"
	"time"
)

func (s *Service) optimizing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conversation.Mode == "optimization"
}
func (s *Service) conversationID() string { s.mu.Lock(); defer s.mu.Unlock(); return s.conversation.ID }
func (s *Service) TradingAction(action func()) error {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	if s.optimizing() {
		return errors.New("策略优化模式禁止交易操作，请先关闭策略优化模式")
	}
	action()
	return nil
}

func (s *Service) SwitchMode(ctx context.Context, mode, expected string) (model.TraderState, error) {
	if mode != "trading" && mode != "optimization" {
		return model.TraderState{}, errors.New("无效代理模式")
	}
	if !s.requestMu.TryLock() {
		return model.TraderState{}, ErrBusy
	}
	defer s.requestMu.Unlock()
	s.mu.Lock()
	c := s.conversation
	s.mu.Unlock()
	if expected != "" && expected != c.ID {
		return model.TraderState{}, store.ErrConversationChanged
	}
	if c.Mode == mode {
		return s.State()
	}
	s.switching.Store(true)
	defer s.switching.Store(false)
	if err := s.StopWithReason("maintenance", "切换代理模式，当前运行已停止"); err != nil {
		return model.TraderState{}, err
	}
	s.mu.Lock()
	cancel := s.replyCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for s.loopActive.Load() || s.replying.Load() || s.scanning.Load() {
		select {
		case <-ctx.Done():
			return model.TraderState{}, ctx.Err()
		case <-deadline.C:
			return model.TraderState{}, errors.New("正在等待当前执行退出，请稍后重新切换")
		case <-tick.C:
		}
	}
	if ctx.Err() != nil {
		return model.TraderState{}, ctx.Err()
	}
	// No old worker remains; switch durable selection and replace local context.
	s.mu.Lock()
	rows, err := s.store.ConversationChat(mode, store.ChatLogKeep)
	if err != nil {
		s.mu.Unlock()
		return model.TraderState{}, err
	}
	next, err := s.store.SwitchConversation(mode, c.ID)
	if err != nil {
		s.mu.Unlock()
		return model.TraderState{}, err
	}
	s.conversation = next
	s.lines = nil
	s.notes = nil
	s.recovering = false
	s.recoverUntil = time.Time{}
	s.seq = 0
	for _, r := range rows {
		s.lines = append(s.lines, ChatLine{ID: r.ID, Role: r.Role, Name: r.Name, Text: r.Text, At: r.CreatedAt, ConversationID: next.ID})
		if r.ID > s.seq {
			s.seq = r.ID
		}
	}
	s.mu.Unlock()
	for {
		select {
		case <-s.wake:
			continue
		default:
			goto drained
		}
	}
drained:
	s.switching.Store(false)
	s.emit(Event{Type: "status", Phase: "conversation_changed", ConversationID: next.ID})
	return s.State()
}

func optimizationTool(name string) bool {
	return name == "get_history" || name == "wait" || store.IsFileTool(name)
}
func (a *agentState) modeDenial(name, args string) string {
	if a.service == nil || !a.service.optimizing() {
		return ""
	}
	if !optimizationTool(name) {
		return "策略优化模式禁止该工具；仅可使用本地历史、文件和等待工具，不会提交交易"
	}
	if name == "create_directory" {
		return "策略优化模式不允许创建文件夹"
	}
	if name == "write_file" {
		var q struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(args), &q) != nil || !(q.Path == prompts.InvestPath || q.Path == "验证.md" || q.Path == "复盘.md") {
			return "策略优化模式仅可写入根目录 INVEST.MD、验证.md 和复盘.md"
		}
	}
	return ""
}

func (s *Service) modeSystemPrompt(st model.Strategy, cfg prompts.Config, tools agenttools.Config, invest string) string {
	mode := "trading"
	if s.optimizing() {
		mode = "optimization"
	}
	return selectedSystemPrompt(st, cfg, tools, invest, mode)
}

func (s *Service) modeInvest() (store.WorkspaceFile, error) {
	s.mu.Lock()
	mode := s.conversation.Mode
	s.mu.Unlock()
	return s.store.ModeInvest(mode)
}
