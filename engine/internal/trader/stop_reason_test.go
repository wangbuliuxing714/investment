package trader

import (
	"context"
	"desk/internal/store"
	"path/filepath"
	"testing"
)

func TestStopReasonSurvivesRestartAndRepeatedStops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stop.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st, nil)
	ctx, cancel := context.WithCancel(context.Background())
	svc.cancel = cancel
	if err = st.SetRunning(true); err != nil {
		t.Fatal(err)
	}
	if err = svc.StopWithReason("maintenance", "engine update"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("loop not canceled")
	}
	first, err := svc.State()
	if err != nil || first.Running || first.StopKind != "maintenance" || first.StopReason != "engine update" || first.StoppedAt == "" {
		t.Fatal(first, err)
	}
	if err = svc.Stop(); err != nil {
		t.Fatal(err)
	}
	second, _ := svc.State()
	if first != second {
		t.Fatal("repeated stop erased cause", first, second)
	}
	if len(svc.Log()) != 1 || svc.Log()[0].Name != "stop_maintenance" {
		t.Fatal(svc.Log())
	}
	st.Close()
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	restored, _ := New(st, nil).State()
	if restored.StopReason != first.StopReason || restored.StoppedAt != first.StoppedAt || restored.Running {
		t.Fatal(restored)
	}
}

func TestManualAndErrorStopsAreDistinct(t *testing.T) {
	for _, kind := range []string{"manual", "error"} {
		svc := batchService(t)
		if err := svc.store.SetRunning(true); err != nil {
			t.Fatal(err)
		}
		if err := svc.StopWithReason(kind, "test cause"); err != nil {
			t.Fatal(err)
		}
		state, _ := svc.State()
		if state.Running || state.StopKind != kind || state.StopReason != "test cause" {
			t.Fatal(state)
		}
	}
}
