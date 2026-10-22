// connection-check verifies the SSH gateway and optionally a model reply.
// It never parses or executes trading tool calls, and never starts the trader.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"desk/internal/admin"
	"desk/internal/agenttools"
	"desk/internal/opencode"
	"desk/internal/prompts"
	"desk/internal/store"
	"github.com/joho/godotenv"
)

func main() {
	modelCheck := flag.Bool("model", false, "check a model reply without executing its output")
	flag.Parse()
	_ = godotenv.Load(".env")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	client := opencode.FromEnv()
	status, err := client.Status(ctx)
	if err != nil {
		fail(err)
	}
	body, _ := json.Marshal(status)
	fmt.Println("gateway=" + string(body))
	if !*modelCheck {
		return
	}
	dbPath := os.Getenv("DESK_DATA")
	if dbPath == "" {
		dbPath = "data/desk.db"
	}
	st, err := store.Open(dbPath)
	if err != nil {
		fail(err)
	}
	defer st.Close()
	state, err := st.Trader()
	if err != nil {
		fail(err)
	}
	if state.Running {
		fail(fmt.Errorf("stop the trader before this diagnostic"))
	}
	settings := st.Settings()
	resolved, err := admin.Resolve(ctx, settings.ProviderID, settings.UpstreamModel)
	if err != nil {
		fail(err)
	}
	cfg, err := st.Prompts()
	if err != nil {
		fail(err)
	}
	strategy, err := st.Strategy()
	if err != nil {
		fail(err)
	}
	tools, err := st.Tools()
	if err != nil {
		fail(err)
	}
	system := prompts.Instructions(cfg, strategy) + "\n\n## 工具上下文\n" + agenttools.Render(tools, strategy)
	reply, err := client.Ask(ctx, opencode.Input{APIURL: resolved.APIURL, APIKey: resolved.APIKey, Model: resolved.Model, Name: settings.ModelName, SystemPrompt: system, Prompt: "这是连接诊断，不是交易任务。不要调用任何工具，不要交易，只回复：连接正常。"})
	if err != nil {
		fail(err)
	}
	if reply == "" {
		fail(fmt.Errorf("empty model reply"))
	}
	fmt.Printf("model_reply_ok=true reply_bytes=%d tools_executed=0\n", len(reply))
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
