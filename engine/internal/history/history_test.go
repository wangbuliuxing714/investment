package history

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"desk/internal/model"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "market.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func bar(ts int64, p float64) model.Candle {
	return model.Candle{Time: ts, Open: p, High: p + 2, Low: p - 1, Close: p + 1, Volume: 3}
}
func zipped(t *testing.T, text string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, e := z.Create("data.csv")
	if e != nil {
		t.Fatal(e)
	}
	f.Write([]byte(text))
	z.Close()
	return b.Bytes()
}

func TestCSVUnitsHeaderAndAnomaly(t *testing.T) {
	body := zipped(t, "open_time,open,high,low,close,volume,close_time\n1502942400000,10,12,9,11,3,0\n1735689600000000,20,22,19,21,4,0\n1512367220799,10,12,9,11,0,0\n")
	rejected := 0
	c, e := parseCSV(body, 1800000000, &rejected)
	if e != nil {
		t.Fatal(e)
	}
	if len(c) != 2 || c[0].Time != 1502942400 || c[1].Time != 1735689600 || rejected != 1 {
		t.Fatalf("bad normalization/audit: %+v %d", c, rejected)
	}
	if _, e = parseCSV(zipped(t, "1502942400000,10,9,8,11,3,0\n"), 1800000000); e == nil {
		t.Fatal("invalid OHLC accepted")
	}
}
func TestAtomicImportIsolationAndPaging(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ts := int64(1735689600)
	rows := []model.Candle{bar(ts, 10), bar(ts+60, 11), bar(ts+120, 12), bar(ts+180, 13)}
	if e := s.put(ctx, "spot", rows, "chunk", "sum"); e != nil {
		t.Fatal(e)
	}
	s.put(ctx, "spot", rows, "chunk", "sum")
	s.put(ctx, "um", []model.Candle{bar(ts, 100)}, "", "")
	invalid := bar(ts+300, 1)
	invalid.Low = -1
	if e := s.put(ctx, "spot", []model.Candle{bar(ts+240, 14), invalid}, "bad", "sum"); e == nil {
		t.Fatal("bad chunk committed")
	}
	d, _ := s.Dataset(ctx, "spot")
	if d.Rows != 4 || d.ArchivesDone != 1 || d.Last != ts+180 {
		t.Fatal(d)
	}
	p, e := s.Query(ctx, "spot", "BTCUSDT", "3m", ts+240, 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Candles) != 1 || p.Candles[0].Time != ts+180 || !p.HasMore {
		t.Fatal(p)
	}
	older, _ := s.Query(ctx, "spot", "BTCUSDT", "3m", p.NextEnd, 1)
	c := older.Candles[0]
	if c.Time != ts || c.Open != 10 || c.Close != 13 || c.High != 14 || c.Low != 9 || c.Volume != 9 || !*c.Closed || older.HasMore {
		t.Fatal(older)
	}
	um, _ := s.Query(ctx, "um", "BTCUSDT", "1m", 0, 10)
	if um.Candles[0].Open != 100 {
		t.Fatal("markets mixed")
	}
}
func TestGapsRemainEmptyAndSeekAcrossOutage(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ts := int64(1735689600)
	s.put(ctx, "spot", []model.Candle{bar(ts, 10), bar(ts+120, 12), bar(ts+86400, 20)}, "", "")
	if e := s.checkGaps(ctx, "spot"); e != nil {
		t.Fatal(e)
	}
	d, _ := s.Dataset(ctx, "spot")
	if d.GapCount != 2 || d.MissingMinutes != 1438 {
		t.Fatal(d)
	}
	p, e := s.Query(ctx, "spot", "BTCUSDT", "1m", ts+3600, 3)
	if e != nil || len(p.Candles) != 2 {
		t.Fatal(p, e)
	}
	p, e = s.Query(ctx, "spot", "BTCUSDT", "3m", ts+180, 1)
	if e != nil || len(p.Candles) != 1 || *p.Candles[0].Closed {
		t.Fatal("missing samples not marked", p, e)
	}
}
func TestCalendarMonthAndMondayWeek(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ts := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC).Unix()
	s.put(ctx, "spot", []model.Candle{bar(ts, 10)}, "", "")
	for interval, want := range map[string]int64{"1M": ts, "1w": time.Date(2025, 1, 27, 0, 0, 0, 0, time.UTC).Unix()} {
		p, e := s.Query(ctx, "spot", "BTCUSDT", interval, ts+60, 1)
		if e != nil || len(p.Candles) != 1 || p.Candles[0].Time != want {
			t.Fatal(interval, p, e)
		}
	}
}

func TestHourlyRollupMatchesRawAndNeverLeaksPastEnd(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	ts := int64(1735689600)
	s.put(ctx, "spot", []model.Candle{bar(ts, 10), bar(ts+120, 12), bar(ts+3600, 20)}, "", "")
	raw, e := s.Query(ctx, "spot", "BTCUSDT", "1h", 0, 100)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.rebuildHours(ctx, "spot"); e != nil {
		t.Fatal(e)
	}
	cached, e := s.Query(ctx, "spot", "BTCUSDT", "1h", 0, 100)
	if e != nil || !reflect.DeepEqual(raw.Candles, cached.Candles) {
		t.Fatal(raw, cached, e)
	}
	cut, e := s.Query(ctx, "spot", "BTCUSDT", "1h", ts+60, 100)
	if e != nil || len(cut.Candles) != 1 || cut.Candles[0].Volume != 3 || cut.Candles[0].Close != 11 {
		t.Fatal("future samples leaked into historical query", cut, e)
	}
	s.put(ctx, "spot", []model.Candle{bar(ts+60, 11)}, "", "")
	dirty, e := s.Query(ctx, "spot", "BTCUSDT", "1h", 0, 100)
	if e != nil || dirty.Candles[0].Volume != 9 {
		t.Fatal("stale rollup used", dirty, e)
	}
	if e = s.rebuildHours(ctx, "spot"); e != nil {
		t.Fatal(e)
	}
	updated, e := s.Query(ctx, "spot", "BTCUSDT", "1h", 0, 100)
	if e != nil || !reflect.DeepEqual(dirty.Candles, updated.Candles) {
		t.Fatal(updated, e)
	}
}
func TestArchiveChecksumAndResume(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	body := zipped(t, "1735689600000,10,12,9,11,3,0\n")
	sum := fmt.Sprintf("%x", sha256.Sum256(body))
	var count atomic.Int32
	bad := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if strings.HasSuffix(r.URL.Path, "CHECKSUM") {
			if bad {
				fmt.Fprint(w, "bad")
			} else {
				fmt.Fprint(w, sum+"  data.zip")
			}
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	syncer := NewSyncer(s)
	syncer.ArchiveBase = srv.URL + "/"
	if e := syncer.archive(ctx, "spot", "data.zip", 1800000000); e == nil {
		t.Fatal("checksum mismatch accepted")
	}
	d, _ := s.Dataset(ctx, "spot")
	if d.Rows != 0 || d.ArchivesDone != 0 {
		t.Fatal("failed chunk was committed")
	}
	bad = false
	if e := syncer.archive(ctx, "spot", "data.zip", 1800000000); e != nil {
		t.Fatal(e)
	}
	n := count.Load()
	if e := syncer.archive(ctx, "spot", "data.zip", 1800000000); e != nil || count.Load() != n {
		t.Fatal("completed chunk refetched", e)
	}
}
func TestSyncRepairsBeginningTailAndInternalGap(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	cutoff := time.Now().UTC().Truncate(time.Minute).Unix()
	first := cutoff - 300
	body := zipped(t, fmt.Sprintf("%d,10,12,9,11,3,0\n%d,12,14,11,13,3,0\n", (first+60)*1000, (first+180)*1000))
	sum := fmt.Sprintf("%x", sha256.Sum256(body))
	key := "data/spot/monthly/klines/BTCUSDT/1m/test.zip"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("prefix") {
			fmt.Fprintf(w, "<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>%s</Key></Contents></ListBucketResult>", key)
			return
		}
		if strings.HasSuffix(r.URL.Path, "CHECKSUM") {
			fmt.Fprint(w, sum)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Write(body)
			return
		}
		q := r.URL.Query()
		start, _ := strconv.ParseInt(q.Get("startTime"), 10, 64)
		end, _ := strconv.ParseInt(q.Get("endTime"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		out := [][]any{}
		for ts := first; ts < cutoff; ts += 60 {
			if ts*1000 >= start && (end == 0 || ts*1000 <= end) && len(out) < limit {
				out = append(out, []any{ts * 1000, "10", "12", "9", "11", "3", ts*1000 + 59999})
			}
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	syncer := NewSyncer(s)
	syncer.ArchiveBase = srv.URL + "/"
	syncer.ListingBase = srv.URL
	syncer.SpotBase = srv.URL
	if e := syncer.Run(ctx, "spot"); e != nil {
		t.Fatal(e)
	}
	d, _ := s.Dataset(ctx, "spot")
	if d.Status != "ready" || d.Rows != 5 || d.GapCount != 0 || d.First != first || d.Last != cutoff-60 || d.SyncedThrough != cutoff {
		t.Fatal(d)
	}
}
