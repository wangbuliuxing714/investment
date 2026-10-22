package trader

import (
	"fmt"
	"strings"
	"time"
)

// Recovery honors only a still-pending recorded wait. A later user message,
// model answer or tool result invalidates it; old orders are never replayed.
func recordedWaitDeadline(lines []ChatLine, now time.Time) time.Time {
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line.Role == "status" {
			continue
		}
		if line.Role == "result" && line.Name == "wait" {
			continue
		}
		if line.Role != "tool" || line.Name != "wait" {
			return time.Time{}
		}
		var seconds int
		if _, err := fmt.Sscanf(strings.TrimSpace(line.Text), "等待 %d 秒", &seconds); err != nil || seconds < int(minWait.Seconds()) || seconds > int(maxWait.Seconds()) {
			return time.Time{}
		}
		at, e := time.Parse(time.RFC3339, line.At)
		if e != nil || at.After(now) {
			return time.Time{}
		}
		until := at.Add(time.Duration(seconds) * time.Second)
		if until.After(now) {
			return until
		}
		return time.Time{}
	}
	return time.Time{}
}
func (s *Service) takeRecovery() (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recovering, until := s.recovering, s.recoverUntil
	s.recovering = false
	s.recoverUntil = time.Time{}
	return recovering, until
}
