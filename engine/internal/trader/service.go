package trader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"desk/internal/admin"
	"desk/internal/ai"
	"desk/internal/binance"
	"desk/internal/history"
	"desk/internal/model"
	"desk/internal/opencode"
	"desk/internal/store"
)

var ErrBusy = errors.New("已有一轮正在执行")
var ErrExchangeBusy = errors.New("请先停止代理并等待当前执行结束，再切换交易环境或密钥")
var errPendingNotes = errors.New("new user input pending")

type Event struct {
	ConversationID string `json:"conversationId,omitempty"`
	Type           string `json:"type"`
	Phase          string `json:"phase,omitempty"`
	DecisionID     int64  `json:"decisionId,omitempty"`
	ID             int64  `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	Text           string `json:"text,omitempty"`
	At             string `json:"at,omitempty"`
	Running        bool   `json:"running"`
}

type ChatLine struct {
	ConversationID string `json:"conversationId,omitempty"`
	ID             int64  `json:"id"`
	Role           string `json:"role"`
	Name           string `json:"name,omitempty"`
	Text           string `json:"text"`
	At             string `json:"at"`
}

type Broadcaster interface {
	Broadcast(any)
}

type remoteAgent interface {
	Round(context.Context, opencode.Input, []json.RawMessage, []any) (ai.Turn, error)
}

type Service struct {
	conversation model.Conversation
	requestMu    sync.Mutex
	switching    atomic.Bool
	replyCancel  context.CancelFunc
	history      *history.Store
	store        *store.Store
	hub          Broadcaster

	mu           sync.Mutex
	cancel       context.CancelFunc
	scanning     atomic.Bool
	loopActive   atomic.Bool
	replying     atomic.Bool
	kick         chan struct{}
	wake         chan struct{}
	gate         sync.Mutex
	remote       remoteAgent
	resolveModel func(context.Context, string, string) (admin.Resolved, error)
	newClient    func(string, string, bool) agentClient
	lines        []ChatLine
	notes        []string
	seq          int64
	recovering   bool
	recoverUntil time.Time
}

// AttachHistory is called before the service starts handling requests.
func (s *Service) AttachHistory(st *history.Store) { s.history = st }

func New(st *store.Store, hub Broadcaster) *Service {
	s := &Service{
		store:        st,
		hub:          hub,
		kick:         make(chan struct{}, 1),
		wake:         make(chan struct{}, 1),
		remote:       nativeAgent{},
		resolveModel: admin.Resolve,
		newClient:    func(key, secret string, demo bool) agentClient { return binance.New(key, secret, demo) },
	}
	if rows, err := st.RecentChat(store.ChatLogKeep); err == nil {
		s.conversation, _ = st.Conversation()
		for _, row := range rows {
			s.lines = append(s.lines, ChatLine{ID: row.ID, Role: row.Role, Name: row.Name, Text: row.Text, At: row.CreatedAt, ConversationID: s.conversation.ID})
			if row.ID > s.seq {
				s.seq = row.ID
			}
		}
	}
	return s
}

func (s *Service) State() (model.TraderState, error) {
	st, err := s.store.Trader()
	if err != nil {
		return st, err
	}
	st.Busy = s.scanning.Load() || s.replying.Load() || (s.loopActive.Load() && !st.Running)
	s.mu.Lock()
	st.Mode = s.conversation.Mode
	st.ConversationID = s.conversation.ID
	s.mu.Unlock()
	st.Switching = s.switching.Load()
	return st, nil
}

func (s *Service) Start() error {
	return s.StartConversation("")
}
func (s *Service) StartConversation(expected string) error {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected != "" && expected != s.conversation.ID {
		return store.ErrConversationChanged
	}
	if s.cancel != nil {
		return nil
	}
	if s.loopActive.Load() || s.replying.Load() || s.scanning.Load() {
		return ErrBusy
	}
	if err := s.store.SetRunning(true); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.loopActive.Store(true)
	go s.loop(ctx)
	s.emit(Event{Type: "status", Phase: "started", Running: true})
	return nil
}

func (s *Service) Stop() error {
	return s.StopWithReason("manual", "收到停止请求（界面或 API）")
}

func (s *Service) StopWithReason(kind, reason string) error {
	labels := map[string]string{"manual": "手动停止", "maintenance": "维护停止", "error": "异常停止", "completed": "本轮完成"}
	label, ok := labels[kind]
	if !ok {
		return errors.New("停止原因类型无效")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = label
	}
	s.mu.Lock()
	if kind == "completed" && len(s.notes) > 0 {
		s.mu.Unlock()
		return errPendingNotes
	}
	st, err := s.store.Trader()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	// A repeated stop request must not overwrite the original stop cause.
	if s.cancel == nil && !st.Running {
		s.mu.Unlock()
		return nil
	}
	if err := s.store.SetStopped(kind, reason); err != nil {
		s.mu.Unlock()
		return err
	}
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.emit(Event{Type: "status", Phase: "stopped", Running: false})
	s.say("status", "stop_"+kind, label+"："+reason)
	return nil
}

func (s *Service) ResumeIfNeeded() {
	st, err := s.store.Trader()
	if err == nil && st.Running {
		until := recordedWaitDeadline(s.Log(), time.Now())
		s.mu.Lock()
		s.recovering, s.recoverUntil = true, until
		s.mu.Unlock()
		_ = s.Start()
	}
}

func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *Service) Ask(text string) error {
	return s.AskConversation(text, "")
}
func (s *Service) AskConversation(text, expected string) error {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	s.mu.Lock()
	changed := expected != "" && expected != s.conversation.ID
	s.mu.Unlock()
	if changed {
		return store.ErrConversationChanged
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("请输入内容")
	}
	if len([]rune(text)) > 1000 {
		return errors.New("一句话不要超过 1000 字")
	}
	s.say("user", "", text)
	s.mu.Lock()
	s.notes = append(s.notes, text)
	running := s.cancel != nil
	startReply := !running && s.replying.CompareAndSwap(false, true)
	s.mu.Unlock()
	if running {
		s.nudge()
		return nil
	}
	if startReply {
		go s.replyOnce()
	}
	return nil
}

func (s *Service) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) takeNotes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.notes) == 0 {
		return nil
	}
	out := append([]string(nil), s.notes...)
	s.notes = nil
	return out
}

func (s *Service) loop(ctx context.Context) {
	defer s.scanning.Store(false)
	defer s.loopActive.Store(false)
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		s.say("status", "", "交易代理内部出错，已停下来")
		_ = s.store.SetScanResult(time.Now().UTC().Format(time.RFC3339), fmt.Sprint(rec))
		_ = s.StopWithReason("error", "交易代理内部错误："+fmt.Sprint(rec))
	}()
	_ = s.store.ClearLastError()
	if s.optimizing() {
		s.say("status", "", "策略优化代理开始运行，仅研究历史与记录优化过程")
	} else {
		s.say("status", "", "交易代理开始运行（原生工具调用）")
	}
	err := s.cloudLoop(ctx)
	if err != nil && ctx.Err() == nil {
		s.say("status", "", err.Error())
		_ = s.store.SetScanResult(time.Now().UTC().Format(time.RFC3339), err.Error())
		_ = s.StopWithReason("error", err.Error())
	}
}

// ConfigureExchange shares the lifecycle lock with Start and Ask so a running
// turn cannot change environment halfway through execution.
func (s *Service) ConfigureExchange(update func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.store.Trader()
	if err != nil {
		return err
	}
	if state.Running || s.cancel != nil || s.loopActive.Load() || s.replying.Load() || s.scanning.Load() {
		return ErrExchangeBusy
	}
	return update()
}

func (s *Service) Log() []ChatLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ChatLine, len(s.lines))
	copy(out, s.lines)
	return out
}

func (s *Service) say(role, name, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if role != "result" || (name != "get_history" && !binance.IsAnalyticsTool(name) && !store.IsFileTool(name)) {
		text = clipChatText(role, text)
	}
	at := time.Now().UTC().Format(time.RFC3339)
	running := s.isRunning()
	s.mu.Lock()
	id, err := s.store.AppendConversationChat(s.conversation.ID, role, name, text, at)
	if err != nil || id == 0 {
		s.seq++
		id = s.seq
	} else if id > s.seq {
		s.seq = id
	}
	line := ChatLine{ID: id, Role: role, Name: name, Text: text, At: at, ConversationID: s.conversation.ID}
	s.lines = append(s.lines, line)
	if len(s.lines) > store.ChatLogKeep {
		s.lines = append([]ChatLine(nil), s.lines[len(s.lines)-store.ChatLogKeep:]...)
	}
	s.mu.Unlock()
	s.emit(Event{Type: "chat", Phase: role, Name: name, Text: text, ID: line.ID, At: line.At, Running: running, ConversationID: line.ConversationID})
}

// clipChatText keeps tool steps and market results intact for the panel.
// Other lines stay short so a long status message cannot fill the log.
func clipChatText(role, text string) string {
	limit := 6000
	if role == "tool" || role == "result" {
		limit = 512000
	}
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "\n…"
}

func (s *Service) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel != nil
}

func (s *Service) emit(ev Event) {
	if s.hub != nil {
		s.hub.Broadcast(ev)
	}
}

func orderDecisions(in []model.Decision) []model.Decision {
	var closes, rest []model.Decision
	for _, d := range in {
		if d.Action == "close_long" || d.Action == "close_short" {
			closes = append(closes, d)
		} else {
			rest = append(rest, d)
		}
	}
	return append(closes, rest...)
}

func sideOf(action string) string {
	if action == "open_short" || action == "close_short" {
		return "short"
	}
	return "long"
}
