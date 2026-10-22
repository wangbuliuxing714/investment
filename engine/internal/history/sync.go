package history

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"desk/internal/model"
)

type Syncer struct {
	Store       *Store
	HTTP        *http.Client
	ArchiveBase string
	ListingBase string
	SpotBase    string
	FuturesBase string
	mu          sync.Mutex
	running     map[string]bool
}

func NewSyncer(s *Store) *Syncer {
	return &Syncer{Store: s, HTTP: &http.Client{Timeout: 90 * time.Second}, ArchiveBase: "https://data.binance.vision/", ListingBase: "https://s3-ap-northeast-1.amazonaws.com/data.binance.vision", SpotBase: "https://api.binance.com", FuturesBase: "https://fapi.binance.com", running: map[string]bool{}}
}
func (s *Syncer) Start(ctx context.Context, m string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ValidMarket(m) || s.running[m] {
		return false
	}
	s.running[m] = true
	go func() {
		defer func() { s.mu.Lock(); delete(s.running, m); s.mu.Unlock() }()
		if err := s.sync(ctx, m); err != nil {
			s.Store.setState(context.Background(), m, "error", err.Error())
		}
	}()
	return true
}
func (s *Syncer) Run(ctx context.Context, m string) error {
	s.mu.Lock()
	if s.running[m] {
		s.mu.Unlock()
		return errors.New("同步已在运行")
	}
	s.running[m] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.running, m); s.mu.Unlock() }()
	err := s.sync(ctx, m)
	if err != nil {
		s.Store.setState(context.Background(), m, "error", err.Error())
	}
	return err
}
func (s *Syncer) Maintain(ctx context.Context) {
	for {
		for _, m := range []string{"spot", "um"} {
			s.Start(ctx, m)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Minute):
		}
	}
}
func (s *Syncer) get(ctx context.Context, u string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if e != nil {
			return nil, e
		}
		res, e := s.HTTP.Do(req)
		if e == nil {
			body, readErr := io.ReadAll(io.LimitReader(res.Body, 32<<20))
			res.Body.Close()
			if res.StatusCode == 200 && readErr == nil {
				return body, nil
			}
			e = fmt.Errorf("HTTP %d %s: %s", res.StatusCode, u, string(body[:min(len(body), 200)]))
			if res.StatusCode == 404 {
				return nil, e
			}
		}
		last = e
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return nil, last
}
func (s *Syncer) listing(ctx context.Context, m string) ([]string, error) {
	prefix := "data/spot/monthly/klines/BTCUSDT/1m/"
	if m == "um" {
		prefix = "data/futures/um/monthly/klines/BTCUSDT/1m/"
	}
	var out []string
	marker := ""
	for {
		q := url.Values{"prefix": {prefix}}
		if marker != "" {
			q.Set("marker", marker)
		}
		body, err := s.get(ctx, s.ListingBase+"?"+q.Encode())
		if err != nil {
			return nil, err
		}
		var listing struct {
			Truncated bool   `xml:"IsTruncated"`
			Next      string `xml:"NextMarker"`
			Contents  []struct {
				Key string `xml:"Key"`
			} `xml:"Contents"`
		}
		if err = xml.Unmarshal(body, &listing); err != nil {
			return nil, err
		}
		for _, item := range listing.Contents {
			if strings.HasPrefix(item.Key, prefix) && strings.HasSuffix(item.Key, ".zip") {
				out = append(out, item.Key)
			}
			marker = item.Key
		}
		if !listing.Truncated {
			break
		}
		if listing.Next != "" {
			marker = listing.Next
		}
		if marker == "" {
			return nil, errors.New("archive listing cursor missing")
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errors.New("官方历史归档列表为空")
	}
	return out, nil
}

func epochSeconds(v int64) int64 {
	if v > 100_000_000_000_000 {
		return v / 1_000_000
	}
	if v > 100_000_000_000 {
		return v / 1000
	}
	return v
}
func parseCSV(body []byte, cutoff int64, rejected ...*int) ([]model.Candle, error) {
	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	if len(r.File) != 1 {
		return nil, errors.New("归档必须只含一个CSV")
	}
	f, err := r.File[0].Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := csv.NewReader(io.LimitReader(f, 64<<20))
	reader.FieldsPerRecord = -1
	out := []model.Candle{}
	line := 0
	for {
		v, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		line++
		if len(v) < 7 {
			return nil, fmt.Errorf("invalid CSV row %d", line)
		}
		ts, e := strconv.ParseInt(v[0], 10, 64)
		if e != nil && line == 1 && (strings.Contains(v[0], "time") || strings.Contains(v[0], "Time")) {
			continue
		}
		if e != nil {
			return nil, e
		}
		c := model.Candle{Time: epochSeconds(ts)}
		// Early official archives contain shifted timestamps. Retain the raw
		// verified archive, omit those rows, and repair against official REST.
		if c.Time%60 != 0 {
			if len(rejected) > 0 {
				*rejected[0]++
			}
			continue
		}
		values := []*float64{&c.Open, &c.High, &c.Low, &c.Close, &c.Volume}
		for i, p := range values {
			*p, e = strconv.ParseFloat(v[i+1], 64)
			if e != nil {
				return nil, e
			}
		}
		if !valid(c) {
			return nil, fmt.Errorf("invalid CSV candle %d", c.Time)
		}
		if c.Time < cutoff {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Syncer) archive(ctx context.Context, m, key string, cutoff int64) error {
	var exists int
	if err := s.Store.db.QueryRowContext(ctx, "SELECT count(*) FROM archives WHERE market=? AND path=?", m, key).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	body, err := s.get(ctx, s.ArchiveBase+key)
	if err != nil {
		return err
	}
	check, err := s.get(ctx, s.ArchiveBase+key+".CHECKSUM")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(check))
	sum := sha256.Sum256(body)
	actual := hex.EncodeToString(sum[:])
	if len(fields) == 0 || !strings.EqualFold(fields[0], actual) {
		return fmt.Errorf("SHA256校验失败：%s", key)
	}
	// Keep the checksum-verified source for independent inspection/reimport.
	path := filepath.Join(s.Store.archiveDir, filepath.FromSlash(key))
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err = os.WriteFile(path, body, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(path+".CHECKSUM", check, 0644); err != nil {
		return err
	}
	rejected := 0
	candles, err := parseCSV(body, cutoff, &rejected)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if len(candles) == 0 {
		return fmt.Errorf("归档无有效历史K线：%s", key)
	}
	if err = s.Store.put(ctx, m, candles, key, actual); err != nil {
		return err
	}
	_, err = s.Store.db.ExecContext(ctx, "INSERT OR REPLACE INTO archive_audit(market,path,rejected) VALUES(?,?,?)", m, key, rejected)
	return err
}

func (s *Syncer) rest(ctx context.Context, m string, start, end int64, limit int) ([]model.Candle, error) {
	base, path := s.SpotBase, "/api/v3/klines"
	if m == "um" {
		base, path = s.FuturesBase, "/fapi/v1/klines"
	}
	q := url.Values{"symbol": {"BTCUSDT"}, "interval": {"1m"}, "limit": {strconv.Itoa(limit)}, "startTime": {strconv.FormatInt(start*1000, 10)}}
	if end > 0 {
		q.Set("endTime", strconv.FormatInt(end*1000-1, 10))
	}
	body, e := s.get(ctx, base+path+"?"+q.Encode())
	if e != nil {
		return nil, e
	}
	var rows [][]json.RawMessage
	if e = json.Unmarshal(body, &rows); e != nil {
		return nil, e
	}
	out := []model.Candle{}
	for _, row := range rows {
		if len(row) < 7 {
			return nil, errors.New("invalid REST candle")
		}
		var ts int64
		if e = json.Unmarshal(row[0], &ts); e != nil {
			return nil, e
		}
		c := model.Candle{Time: epochSeconds(ts)}
		for i, p := range []*float64{&c.Open, &c.High, &c.Low, &c.Close, &c.Volume} {
			var v string
			if e = json.Unmarshal(row[i+1], &v); e != nil {
				return nil, e
			}
			*p, e = strconv.ParseFloat(v, 64)
			if e != nil {
				return nil, e
			}
		}
		if !valid(c) {
			return nil, errors.New("invalid REST OHLCV")
		}
		if end == 0 || c.Time < end {
			out = append(out, c)
		}
	}
	return out, nil
}
func (s *Syncer) fill(ctx context.Context, m string, start, end int64) error {
	for start < end {
		rows, err := s.rest(ctx, m, start, end, 1000)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		if rows[0].Time < start {
			return errors.New("历史接口分页未向前推进")
		}
		if err = s.Store.put(ctx, m, rows, "", ""); err != nil {
			return err
		}
		next := rows[len(rows)-1].Time + 60
		if next <= start {
			return errors.New("历史接口游标未推进")
		}
		start = next
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(120 * time.Millisecond):
		}
	}
	return nil
}
func (s *Syncer) sync(ctx context.Context, m string) error {
	if !ValidMarket(m) {
		return errors.New("invalid market")
	}
	if err := s.Store.setState(ctx, m, "syncing", ""); err != nil {
		return err
	}
	cutoff := time.Now().UTC().Truncate(time.Minute).Unix()
	first, err := s.rest(ctx, m, 0, cutoff, 1)
	if err != nil {
		return err
	}
	if len(first) != 1 {
		return errors.New("无法确认交易所第一根K线")
	}
	keys, err := s.listing(ctx, m)
	if err != nil {
		return err
	}
	if _, err = s.Store.db.ExecContext(ctx, "UPDATE datasets SET available_first=?,archives_total=? WHERE market=?", first[0].Time, len(keys), m); err != nil {
		return err
	}
	// A small worker pool keeps network latency down while SQLite serializes chunk commits.
	jobs := make(chan string)
	errs := make(chan error, 3)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				if e := s.archive(workerCtx, m, key, cutoff); e != nil {
					select {
					case errs <- e:
					default:
					}
					cancel()
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, key := range keys {
			select {
			case jobs <- key:
			case <-workerCtx.Done():
				return
			}
		}
	}()
	wg.Wait()
	select {
	case err = <-errs:
		return err
	default:
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	d, err := s.Store.Dataset(ctx, m)
	if err != nil {
		return err
	}
	if d.First > first[0].Time {
		if err = s.fill(ctx, m, first[0].Time, d.First); err != nil {
			return err
		}
	}
	d, err = s.Store.Dataset(ctx, m)
	if err != nil {
		return err
	}
	if err = s.fill(ctx, m, d.Last+60, cutoff); err != nil {
		return err
	}
	if err = s.Store.checkGaps(ctx, m); err != nil {
		return err
	}
	// Retry internal archive gaps against the official REST source. Missing exchange
	// minutes remain explicit gaps; no synthetic candles or forward filling.
	gaps, err := s.Store.Gaps(ctx, m)
	if err != nil {
		return err
	}
	for _, gap := range gaps {
		if err = s.fill(ctx, m, gap.From, gap.To+60); err != nil {
			return err
		}
	}
	if len(gaps) > 0 {
		if err = s.Store.checkGaps(ctx, m); err != nil {
			return err
		}
	}
	d, err = s.Store.Dataset(ctx, m)
	if err != nil {
		return err
	}
	if d.Last+60 < cutoff {
		return errors.New("最新历史尚未补齐，请重试同步")
	}
	if d.First != first[0].Time {
		return errors.New("历史起点尚未补齐，请重试同步")
	}
	if err = s.Store.rebuildHours(ctx, m); err != nil {
		return err
	}
	if _, err = s.Store.db.ExecContext(ctx, "UPDATE datasets SET synced_through=? WHERE market=?", cutoff, m); err != nil {
		return err
	}
	return s.Store.setState(ctx, m, "ready", "")
}
