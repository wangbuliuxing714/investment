package prompts

import (
	"reflect"
	"strings"
	"testing"

	"desk/internal/model"
)

func TestStructuredDefinitionsAndRenderedInstructions(t *testing.T) {
	c := Defaults()
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	for _, d := range Definitions {
		if !strings.HasPrefix(d.Default, "## ") {
			t.Fatal("unstructured default", d.Key)
		}
		if d.Scenario == "" || d.Placement == "" || d.EffectiveWhen == "" {
			t.Fatal("missing usage", d.Key)
		}
		if (d.Role == "system") != (d.Key == "system" || d.Key == "trading" || d.Key == "optimization") {
			t.Fatal("wrong message role", d.Key)
		}
		if d.Role == "event" && d.Category != "automation" {
			t.Fatal("wrong event category", d.Key)
		}
	}
	st := model.Strategy{Coins: []string{"BTCUSDT"}, BTCETHLeverage: 5, AltLeverage: 3, MaxPositions: 2, PositionValueRatio: 1, MaxMarginUsage: 0.5, MinConfidence: 75}
	c["system"] += "\ncustom text {{coins}}"
	body := Instructions(c, st)
	if strings.Contains(body, "{{") {
		t.Fatal("unresolved trading variables")
	}
	want := Render(c["system"], st)
	if body != want {
		t.Fatal("request composition or strategy preservation changed")
	}
	if _, ok := c["tools"]; ok {
		t.Fatal("tools still stored in model prompts")
	}
	if strings.Contains(body, "决策输出协议") || strings.Contains(body, "position_size_usd") {
		t.Fatal("tool protocol still in system template")
	}
}

func TestUpgradeOnlyExactStockDefaults(t *testing.T) {
	for _, strategy := range []string{"", "my original strategy"} {
		c := LegacyDefaults()
		for key, text := range legacyDefaultsV1 {
			c[key] = text
		}
		c["strategy"] = strategy
		c["system"] = "my custom system"
		c["tools"] += "\n" // Even a whitespace edit is custom.
		original := Config{}
		for k, v := range c {
			original[k] = v
		}
		next, changed := UpgradeStockDefaultsV2(c)
		if !changed || next["trading"] != LegacyDefaults()["trading"] {
			t.Fatal("stock text was not upgraded")
		}
		for _, key := range []string{"system", "tools", "strategy"} {
			if next[key] != original[key] {
				t.Fatal("custom text changed", key)
			}
		}
		if !reflect.DeepEqual(c, original) {
			t.Fatal("input map mutated")
		}
		again, changed := UpgradeStockDefaultsV2(next)
		if changed || !reflect.DeepEqual(again, next) {
			t.Fatal("upgrade is not idempotent")
		}
	}
}
