package trader

import "testing"

func TestFloorToStep(t *testing.T) {
	if got := FloorToStep(0.0019, "0.001"); got != "0.001" {
		t.Fatalf("got %s", got)
	}
	if got := FloorToStep(0.0004, "0.001"); got != "0" {
		t.Fatalf("got %s", got)
	}
	if got := FloorToStep(64123.9, "0.10"); got != "64123.9" {
		t.Fatalf("got %s", got)
	}
	if stepDecimals("0.010") != 2 {
		t.Fatal(stepDecimals("0.010"))
	}
}
