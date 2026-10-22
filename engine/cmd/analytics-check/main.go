package main

// Read-only smoke check: only demo account credentials are read; all exchange requests are GET.
import (
	"context"
	"database/sql"
	"desk/internal/binance"
	"encoding/json"
	"flag"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func main() {
	path := flag.String("db", "", "existing database (opened read-only)")
	output := flag.String("out", "", "optional result JSON file")
	reference := flag.Bool("reference", false, "explicitly test live public reference OI/order-flow")
	flag.Parse()
	if *path == "" {
		panic("-db required")
	}
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(*path)+"?mode=ro")
	if e != nil {
		panic(e)
	}
	defer db.Close()
	var mode, key, secret string
	e = db.QueryRow(`SELECT COALESCE(MAX(CASE WHEN key='binance_connection' THEN value END),''),COALESCE(MAX(CASE WHEN key='binance_demo_api_key' THEN value END),''),COALESCE(MAX(CASE WHEN key='binance_demo_api_secret' THEN value END),'') FROM settings`).Scan(&mode, &key, &secret)
	if e != nil {
		panic(e)
	}
	if mode != "demo" {
		panic("smoke test requires active demo environment")
	}
	client := binance.New(key, secret, true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	names := []string{"get_indicators", "get_trading_history", "get_funding", "get_open_interest", "get_order_flow", "get_order_book"}
	results := make([]binance.AnalyticsResult, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			r := binance.AnalyticsRequest{Symbol: "BTCUSDT"}
			if name == "get_order_book" {
				r.NotionalUSD = 100
			}
			if *reference && (name == "get_open_interest" || name == "get_order_flow") {
				r.Environment = "live_reference"
			}
			results[i] = client.Analytics(ctx, name, r)
		}(i, name)
	}
	wg.Wait()
	if *output != "" {
		b, e := json.MarshalIndent(results, "", "  ")
		if e != nil {
			panic(e)
		}
		if e = os.WriteFile(*output, b, 0600); e != nil {
			panic(e)
		}
	}
	for _, r := range results {
		keys := []string{}
		for k := range r.Data {
			keys = append(keys, k)
		}
		b, _ := json.Marshal(map[string]any{"tool": r.Tool, "status": r.Status, "environment": r.Source.Environment, "sections": keys, "errors": r.Errors})
		fmt.Println(string(b))
	}
}
