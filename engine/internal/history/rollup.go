package history

import "context"

// Hourly bars accelerate long-range views. Rebuild from the earliest changed
// hour, retaining the original sample count so missing minutes stay visible.
func (s *Store) rebuildHours(ctx context.Context, m string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Acquire the writer before reading the watermark (another market may sync).
	if _, err = tx.ExecContext(ctx, "UPDATE hourly_state SET dirty=dirty WHERE market=?", m); err != nil {
		return err
	}
	var dirty int64
	if err = tx.QueryRowContext(ctx, "SELECT dirty FROM hourly_state WHERE market=?", m).Scan(&dirty); err != nil {
		return err
	}
	if dirty == 0 {
		return nil
	}
	dirty = dirty / 3600 * 3600
	if _, err = tx.ExecContext(ctx, "DELETE FROM hourly WHERE market=? AND time>=?", m, dirty); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO hourly(market,time,open,high,low,close,volume,samples)
 WITH g AS(SELECT time/3600*3600 bucket,min(time) first,max(time) last,max(high) high,min(low) low,sum(volume) volume,count(*) samples FROM candles WHERE market=? AND time>=? GROUP BY bucket)
 SELECT ?,g.bucket,f.open,g.high,g.low,l.close,g.volume,g.samples FROM g JOIN candles f ON f.market=? AND f.time=g.first JOIN candles l ON l.market=? AND l.time=g.last`, m, dirty, m, m, m)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE hourly_state SET dirty=0,built=1 WHERE market=?", m); err != nil {
		return err
	}
	return tx.Commit()
}
