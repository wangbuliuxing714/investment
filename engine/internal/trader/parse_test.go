package trader

import "testing"

func TestParseFencedJSON(t *testing.T) {
	raw := "方向偏多，但只做一笔。\n```json\n{\"reasoning\":\"等回踩\",\"decisions\":[{\"symbol\":\"btcusdt\",\"action\":\"开多\",\"leverage\":8,\"position_size_usd\":200,\"stop_loss\":1,\"take_profit\":2,\"confidence\":80}]}\n```"
	reasoning, decisions, err := ParseResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(reasoning, "方向偏多") || !contains(reasoning, "等回踩") {
		t.Fatalf("reasoning %q", reasoning)
	}
	if len(decisions) != 1 || decisions[0].Action != "open_long" || decisions[0].Symbol != "BTCUSDT" || decisions[0].PositionSizeUSD != 200 {
		t.Fatalf("%+v", decisions)
	}
}

func TestParseArray(t *testing.T) {
	_, decisions, err := ParseResponse(`[{"symbol":"ETHUSDT","action":"hold"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Action != "hold" {
		t.Fatalf("%+v", decisions)
	}
}

func TestParseInvalid(t *testing.T) {
	_, _, err := ParseResponse("```json\n{not json}\n```")
	if err == nil {
		t.Fatal("expected error")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (func() bool { return stringIndex(s, sub) >= 0 })()))
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
