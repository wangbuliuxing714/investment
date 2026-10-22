package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"desk/internal/admin"

	"github.com/joho/godotenv"
)

func TestLiveDeskOpenCode(t *testing.T) {
	if os.Getenv("DESK_OPENCODE_LIVE") != "1" {
		t.Skip("set DESK_OPENCODE_LIVE=1 to check the server process")
	}
	_ = godotenv.Load(filepath.Join("..", "..", ".env"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := FromEnv()
	before, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.P8790 == 0 || before.Gateway != "active" {
		t.Fatalf("novel worker not healthy before change: %+v", before)
	}

	settings, err := http.Get("http://127.0.0.1:8787/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Body.Close()
	var view struct {
		ModelName     string `json:"modelName"`
		ProviderID    string `json:"providerId"`
		UpstreamModel string `json:"upstreamModel"`
	}
	if err := json.NewDecoder(settings.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	resolved, err := admin.Resolve(ctx, view.ProviderID, view.UpstreamModel)
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Ask(ctx, Input{
		APIURL: resolved.APIURL,
		APIKey: resolved.APIKey,
		Model:  resolved.Model,
		Name:   view.ModelName,
		Prompt: "只回复一个汉字：好",
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.P8790 != before.P8790 || after.Novel == "active" || after.Gateway != "active" || after.Desk != "active" || after.P8793 == 0 {
		t.Fatalf("process check failed before=%+v after=%+v", before, after)
	}
	if len(text) == 0 || len(text) > 200 {
		t.Fatalf("unexpected reply length %d", len(text))
	}
	t.Logf("desk=%s novel_pid=%d reply_len=%d", after.Desk, after.P8790, len(text))
}
