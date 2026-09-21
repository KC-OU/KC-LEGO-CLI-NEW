package lego

import (
	"context"
	"fmt"
	"time"
)

// Shared Rebrickable bookkeeping in lego.db: one "next request allowed at"
// timestamp all processes queue behind, and a response cache. Every helper
// takes the database write lock for a moment, so concurrent sessions can't
// both decide the same instant is free.

const metaNextAllowed = "next_allowed_ms"

// reserveSlot claims the next free request slot at least interval after the
// previous one and returns how long the caller must wait for it.
func (d *DB) reserveSlot(interval time.Duration) (time.Duration, error) {
	return d.reserveSlotKey(metaNextAllowed, interval)
}

// reserveSlotKey is reserveSlot for any named API (each API queues behind its own timestamp).
func (d *DB) reserveSlotKey(key string, interval time.Duration) (time.Duration, error) {
	ctx := context.Background()
	conn, err := d.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, err
	}
	ok := false
	defer func() {
		if !ok {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	now := time.Now().UnixMilli()
	var next int64
	_ = conn.QueryRowContext(ctx, "SELECT value FROM rb_meta WHERE key = ?", key).Scan(&next)
	slot := now
	if next > slot {
		slot = next
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO rb_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, slot+interval.Milliseconds()); err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return 0, err
	}
	ok = true
	return time.Duration(slot-now) * time.Millisecond, nil
}

// pushCooldown records that Rebrickable throttled us until t, so every process
// (not just the one that got the 429) stays quiet until then.
func (d *DB) pushCooldown(t time.Time) error {
	_, err := d.Exec(`INSERT INTO rb_meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = MAX(value, excluded.value)`, metaNextAllowed, t.UnixMilli())
	return err
}

// cacheGet returns a stored response if it is younger than ttl; a stored 404
// is only trusted for notFoundTTL however long ttl is.
func (d *DB) cacheGet(key string, ttl time.Duration) (status int, body []byte, ok bool) {
	var fetched int64
	if err := d.QueryRow("SELECT status, body, fetched_at FROM rb_cache WHERE key = ?", key).Scan(&status, &body, &fetched); err != nil {
		return 0, nil, false
	}
	if status == 404 && ttl > notFoundTTL {
		ttl = notFoundTTL
	}
	if time.Since(time.UnixMilli(fetched)) > ttl {
		return 0, nil, false
	}
	return status, body, true
}

func (d *DB) cachePut(key string, status int, body []byte) {
	_, _ = d.Exec(`INSERT INTO rb_cache (key, status, body, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET status = excluded.status, body = excluded.body, fetched_at = excluded.fetched_at`,
		key, status, body, time.Now().UnixMilli())
}

// CacheStats reports how many Rebrickable responses are cached, for `wms doctor`.
func (d *DB) CacheStats() (entries int, err error) {
	if err := d.QueryRow("SELECT COUNT(*) FROM rb_cache").Scan(&entries); err != nil {
		return 0, fmt.Errorf("reading rebrickable cache: %w", err)
	}
	return entries, nil
}
