package trader

import (
	"context"
	"desk/internal/admin"
	"testing"
	"time"
)

func TestRecordedWaitRecoveryInvalidation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	wait := ChatLine{Role: "tool", Name: "wait", Text: "等待 120 秒（watch）", At: now.Add(-30 * time.Second).Format(time.RFC3339)}
	lines := []ChatLine{wait, {Role: "status", Text: "waiting"}, {Role: "status", Text: "maintenance"}}
	if d := recordedWaitDeadline(lines, now).Sub(now); d != 90*time.Second {
		t.Fatal(d)
	}
	for _, line := range []ChatLine{{Role: "user"}, {Role: "agent"}, {Role: "tool", Name: "get_market"}, {Role: "result", Name: "submit_decisions"}} {
		if !recordedWaitDeadline(append(lines, line), now).IsZero() {
			t.Fatal("new event did not invalidate", line)
		}
	}
	if !recordedWaitDeadline(lines, now.Add(5*time.Minute)).IsZero() {
		t.Fatal("expired wait restored")
	}
}
func TestResumeKeepsRunningAndWaitWithoutModelCall(t *testing.T) {
	s := batchService(t)
	s.say("tool", "wait", "等待 120 秒（watch）")
	if e := s.store.SetRunning(true); e != nil {
		t.Fatal(e)
	}
	called := make(chan struct{}, 1)
	s.resolveModel = func(context.Context, string, string) (admin.Resolved, error) {
		called <- struct{}{}
		return admin.Resolved{}, nil
	}
	s.ResumeIfNeeded()
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		for _, line := range s.Log() {
			if line.Name == "engine_resume" {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !found {
		t.Fatal("not resumed into previous wait")
	}
	state, _ := s.State()
	if !state.Running || state.Busy {
		t.Fatal(state)
	}
	select {
	case <-called:
		t.Fatal("model called before previous wait ended")
	default:
	}
	if e := s.Stop(); e != nil {
		t.Fatal(e)
	}
	for s.loopActive.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.loopActive.Load() {
		t.Fatal("recovered wait cannot be stopped")
	}
}
