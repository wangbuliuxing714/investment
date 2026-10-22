// Package history owns the public-market archive, separate from trading state.
package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"desk/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct {
	db         *sql.DB
	archiveDir string
}
type Dataset struct {
	Market         string `json:"market"`
	Symbol         string `json:"symbol"`
	Source         string `json:"source"`
	Status         string `json:"status"`
	Rows           int64  `json:"rows"`
	First          int64  `json:"first"`
	Last           int64  `json:"last"`
	AvailableFirst int64  `json:"availableFirst"`
	SyncedThrough  int64  `json:"syncedThrough"`
	ArchivesDone   int    `json:"archivesDone"`
	ArchivesTotal  int    `json:"archivesTotal"`
	GapCount       int64  `json:"gapCount"`
	MissingMinutes int64  `json:"missingMinutes"`
	UpdatedAt      string `json:"updatedAt"`
	Error          string `json:"error"`
}
type Gap struct {
	From    int64 `json:"from"`
	To      int64 `json:"to"`
	Minutes int64 `json:"minutes"`
}
type Page struct {
	Candles []model.Candle `json:"candles"`
	Dataset Dataset        `json:"dataset"`
	NextEnd int64          `json:"nextEnd"`
	HasMore bool           `json:"hasMore"`
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	s := &Store{db: db, archiveDir: filepath.Join(filepath.Dir(path), "market-archives")}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS datasets(
 market TEXT PRIMARY KEY,symbol TEXT NOT NULL DEFAULT 'BTCUSDT',status TEXT NOT NULL DEFAULT 'pending',
 rows INTEGER NOT NULL DEFAULT 0,first INTEGER NOT NULL DEFAULT 0,last INTEGER NOT NULL DEFAULT 0,
 available_first INTEGER NOT NULL DEFAULT 0,synced_through INTEGER NOT NULL DEFAULT 0,
 archives_done INTEGER NOT NULL DEFAULT 0,archives_total INTEGER NOT NULL DEFAULT 0,
 gap_count INTEGER NOT NULL DEFAULT 0,missing_minutes INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS candles(market TEXT NOT NULL,time INTEGER NOT NULL,open REAL NOT NULL,high REAL NOT NULL,low REAL NOT NULL,close REAL NOT NULL,volume REAL NOT NULL,PRIMARY KEY(market,time)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS archives(market TEXT NOT NULL,path TEXT NOT NULL,checksum TEXT NOT NULL,PRIMARY KEY(market,path)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS archive_audit(market TEXT NOT NULL,path TEXT NOT NULL,rejected INTEGER NOT NULL,PRIMARY KEY(market,path)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS hourly(market TEXT NOT NULL,time INTEGER NOT NULL,open REAL NOT NULL,high REAL NOT NULL,low REAL NOT NULL,close REAL NOT NULL,volume REAL NOT NULL,samples INTEGER NOT NULL,PRIMARY KEY(market,time)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS hourly_state(market TEXT PRIMARY KEY,dirty INTEGER NOT NULL DEFAULT 0,built INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS gaps(market TEXT NOT NULL,start INTEGER NOT NULL,end INTEGER NOT NULL,minutes INTEGER NOT NULL,PRIMARY KEY(market,start)) WITHOUT ROWID;
 INSERT OR IGNORE INTO datasets(market) VALUES('spot'),('um');
 INSERT OR IGNORE INTO hourly_state(market,dirty) SELECT market,first FROM datasets;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error   { return s.db.Close() }
func ValidMarket(m string) bool { return m == "spot" || m == "um" }
func (s *Store) Dataset(ctx context.Context, m string) (Dataset, error) {
	return readDataset(ctx, s.db, m)
}

type rowReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readDataset(ctx context.Context, db rowReader, m string) (Dataset, error) {
	d := Dataset{Market: m, Source: "Binance official public market · 1m"}
	err := db.QueryRowContext(ctx, `SELECT symbol,status,rows,first,last,available_first,synced_through,archives_done,archives_total,gap_count,missing_minutes,updated_at,error FROM datasets WHERE market=?`, m).Scan(&d.Symbol, &d.Status, &d.Rows, &d.First, &d.Last, &d.AvailableFirst, &d.SyncedThrough, &d.ArchivesDone, &d.ArchivesTotal, &d.GapCount, &d.MissingMinutes, &d.UpdatedAt, &d.Error)
	return d, err
}
func (s *Store) Datasets(ctx context.Context) ([]Dataset, error) {
	out := []Dataset{}
	for _, m := range []string{"spot", "um"} {
		d, e := s.Dataset(ctx, m)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, nil
}
func (s *Store) setState(ctx context.Context, m, status, message string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE datasets SET status=?,error=?,updated_at=? WHERE market=?", status, message, time.Now().UTC().Format(time.RFC3339), m)
	return err
}

func valid(c model.Candle) bool {
	for _, v := range []float64{c.Open, c.High, c.Low, c.Close, c.Volume} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return c.Time > 0 && c.Time%60 == 0 && c.Open > 0 && c.Close > 0 && c.Low > 0 && c.High >= math.Max(c.Open, c.Close) && c.Low <= math.Min(c.Open, c.Close) && c.Volume >= 0
}

// A chunk and its completion marker commit atomically, allowing interrupted imports to resume.
func (s *Store) put(ctx context.Context, m string, candles []model.Candle, archive, checksum string) error {
	if !ValidMarket(m) {
		return errors.New("invalid market")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO candles(market,time,open,high,low,close,volume) VALUES(?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	var added, first, last int64
	for _, c := range candles {
		if !valid(c) {
			return fmt.Errorf("invalid candle at %d", c.Time)
		}
		r, e := stmt.ExecContext(ctx, m, c.Time, c.Open, c.High, c.Low, c.Close, c.Volume)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		added += n
		if first == 0 || c.Time < first {
			first = c.Time
		}
		if c.Time > last {
			last = c.Time
		}
	}
	if first > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE datasets SET rows=rows+?,first=CASE WHEN first=0 THEN ? ELSE min(first,?) END,last=max(last,?),updated_at=? WHERE market=?`, added, first, first, last, time.Now().UTC().Format(time.RFC3339), m)
		if err != nil {
			return err
		}
		if added > 0 {
			if _, err = tx.ExecContext(ctx, "UPDATE hourly_state SET dirty=CASE WHEN dirty=0 THEN ? ELSE min(dirty,?) END WHERE market=?", first/3600*3600, first/3600*3600, m); err != nil {
				return err
			}
		}
	}
	if archive != "" {
		if _, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO archives(market,path,checksum) VALUES(?,?,?)", m, archive, checksum); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE datasets SET archives_done=(SELECT count(*) FROM archives WHERE market=?) WHERE market=?", m, m); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) checkGaps(ctx context.Context, m string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM gaps WHERE market=?", m); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO gaps(market,start,end,minutes) SELECT ?,previous+60,time-60,(time-previous)/60-1 FROM (SELECT time,lag(time) OVER(ORDER BY time) previous FROM candles WHERE market=?) WHERE time-previous>60`, m, m); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE datasets SET gap_count=(SELECT count(*) FROM gaps WHERE market=?),missing_minutes=(SELECT coalesce(sum(minutes),0) FROM gaps WHERE market=?) WHERE market=?`, m, m, m)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Gaps(ctx context.Context, m string) ([]Gap, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT start,end,minutes FROM gaps WHERE market=? ORDER BY start LIMIT 500", m)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Gap{}
	for rows.Next() {
		var g Gap
		if e = rows.Scan(&g.From, &g.To, &g.Minutes); e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

var steps = map[string]int64{"1m": 60, "3m": 180, "5m": 300, "15m": 900, "30m": 1800, "1h": 3600, "2h": 7200, "4h": 14400, "6h": 21600, "8h": 28800, "12h": 43200, "1d": 86400, "3d": 259200, "1w": 604800}

func ValidInterval(i string) bool { return steps[i] > 0 || i == "1M" }

func (s *Store) Query(ctx context.Context, m, symbol, interval string, end int64, limit int) (Page, error) {
	out := Page{Candles: []model.Candle{}}
	if !ValidMarket(m) || symbol != "BTCUSDT" || !ValidInterval(interval) || limit < 1 || limit > 5000 || end < 0 {
		return out, errors.New("历史库支持 BTCUSDT 现货/永续、1分钟至月线，单次1–5000根")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	d, err := readDataset(ctx, tx, m)
	if err != nil {
		return out, err
	}
	out.Dataset = d
	if end >= 1_000_000_000_000 {
		end /= 1000
	}
	if end == 0 {
		end = d.Last + 60
	}
	// Seek across empty source periods without inventing candles. Preserve the
	// requested end within the last populated bucket for accurate partial bars.
	var last sql.NullInt64
	if err = tx.QueryRowContext(ctx, "SELECT max(time) FROM candles WHERE market=? AND time<?", m, end).Scan(&last); err != nil {
		return out, err
	}
	if !last.Valid {
		return out, nil
	}
	if end-last.Int64 > 60 {
		end = last.Int64 + 60
	}
	step := steps[interval]
	source := "candles"
	sampleExpr := "1"
	if (step >= 3600 || interval == "1M") && (end%3600 == 0 || end >= d.Last+60) {
		var ready int
		if err = tx.QueryRowContext(ctx, "SELECT built=1 AND dirty=0 FROM hourly_state WHERE market=?", m).Scan(&ready); err != nil {
			return out, err
		}
		if ready == 1 {
			source = "hourly"
			sampleExpr = "samples"
		}
	}
	var lower int64
	expr := ""
	if interval == "1M" {
		t := time.Unix(end, 0).UTC()
		lower = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -limit, 0).Unix()
		expr = "CAST(strftime('%s',time,'unixepoch','start of month') AS INTEGER)"
	} else {
		anchor := int64(0)
		if interval == "1w" {
			anchor = 345600
		}
		lower = ((end-anchor)/step-int64(limit))*step + anchor
		expr = fmt.Sprintf("((time-%d)/%d)*%d+%d", anchor, step, step, anchor)
	}
	// Bounds are aligned before aggregation so the oldest bucket is never cut by pagination.
	q := `WITH b AS (SELECT time,high,low,volume,` + sampleExpr + ` samples,` + expr + ` bucket FROM ` + source + ` WHERE market=? AND time>=? AND time<?),g AS (SELECT bucket,min(time) first,max(time) last,max(high) high,min(low) low,sum(volume) volume,sum(samples) samples FROM b GROUP BY bucket ORDER BY bucket DESC LIMIT ?) SELECT g.bucket,f.open,g.high,g.low,l.close,g.volume,g.samples FROM g JOIN ` + source + ` f ON f.market=? AND f.time=g.first JOIN ` + source + ` l ON l.market=? AND l.time=g.last ORDER BY g.bucket`
	rows, err := tx.QueryContext(ctx, q, m, lower, end, limit, m, m)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c model.Candle
		var samples int64
		if err = rows.Scan(&c.Time, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &samples); err != nil {
			return out, err
		}
		next := c.Time + step
		if interval == "1M" {
			next = time.Unix(c.Time, 0).UTC().AddDate(0, 1, 0).Unix()
		}
		c.CloseTime = next*1000 - 1
		closed := next <= time.Now().Unix() && samples == (next-c.Time)/60
		c.Closed = &closed
		c.Source = "local-binance-" + m
		out.Candles = append(out.Candles, c)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Candles) > 0 {
		out.NextEnd = out.Candles[0].Time
		out.HasMore = d.First < out.NextEnd
	}
	return out, nil
}

func ParseMarket(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
