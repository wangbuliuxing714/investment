package opencode

import "testing"

func TestConversationSwitchDropsRemoteModelContext(t *testing.T) {
	c := &Client{conversationKey: "trading", sessionID: "trade-session", sessionModel: "model"}
	c.selectConversation("trading")
	if c.sessionID != "trade-session" {
		t.Fatal("same conversation reset")
	}
	c.selectConversation("optimization")
	if c.sessionID != "" || c.conversationKey != "optimization" {
		t.Fatal("trade context retained")
	}
	c.sessionID = "research-session"
	c.selectConversation("trading")
	if c.sessionID != "" {
		t.Fatal("research leaked back to trading")
	}
}
