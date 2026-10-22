package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"desk/internal/history"
	"desk/internal/httpapi"
	"desk/internal/store"
	"desk/internal/trader"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load(".env")
	_ = godotenv.Load(filepath.Join("engine", ".env"))

	listen := getenv("DESK_LISTEN", "127.0.0.1:8787")
	data := getenv("DESK_DATA", filepath.Join("data", "desk.db"))
	st, err := store.Open(data)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	market, err := history.Open(filepath.Join(filepath.Dir(data), "market.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer market.Close()
	syncer := history.NewSyncer(market)

	hub := httpapi.NewHub()
	svc := trader.New(st, hub)
	svc.AttachHistory(market)
	svc.ResumeIfNeeded()
	server := httpapi.New(st, svc, hub)
	server.AttachHistory(market, syncer)
	go syncer.Maintain(context.Background())

	log.Printf("交易台引擎 http://%s", listen)
	if err := http.ListenAndServe(listen, server.Handler()); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
