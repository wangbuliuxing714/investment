package main

import (
	"context"
	"desk/internal/history"
	"flag"
	"log"
	"sync"
	"time"
)

func main() {
	path := flag.String("data", "data/market.db", "history database")
	market := flag.String("market", "", "spot or um; empty runs both")
	flag.Parse()
	s, err := history.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer := history.NewSyncer(s)
	var wg sync.WaitGroup
	markets := []string{"spot", "um"}
	if *market != "" {
		if !history.ValidMarket(*market) {
			log.Fatal("invalid market")
		}
		markets = []string{*market}
	}
	for _, m := range markets {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			if e := syncer.Run(ctx, m); e != nil {
				log.Printf("%s ERROR %v", m, e)
			} else {
				d, _ := s.Dataset(ctx, m)
				log.Printf("%s READY rows=%d first=%s last=%s gaps=%d", m, d.Rows, time.Unix(d.First, 0).UTC(), time.Unix(d.Last, 0).UTC(), d.GapCount)
			}
		}(m)
	}
	wg.Wait()
	rows, _ := s.Datasets(ctx)
	for _, d := range rows {
		if *market != "" && d.Market != *market {
			continue
		}
		if d.Status != "ready" {
			log.Fatal("history import incomplete; rerun to resume")
		}
	}
}
