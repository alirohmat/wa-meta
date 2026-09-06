package main

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

var stateCache sync.Map

type stateWriteJob struct {
	Key       string
	Value     string
	ExpiresAt int64
}

func setupStateTable(db *sql.DB) error {
	if isPostgres() {
		_, err := db.Exec(`CREATE TABLE IF NOT EXISTS bot_state(key TEXT PRIMARY KEY, value TEXT NOT NULL, expires_at BIGINT NOT NULL)`)
		if err != nil {
			return err
		}
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_bot_state_expires ON bot_state(expires_at)`)
		return err
	}

	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return err
	}
	_, _ = db.Exec(`PRAGMA synchronous=NORMAL;`)
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS bot_state(key TEXT PRIMARY KEY, value TEXT NOT NULL, expires_at INTEGER NOT NULL);`)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_bot_state_expires ON bot_state(expires_at);`)
	return nil
}

func (b *bridge) LoadStateOnStartup() error {
	var rows *sql.Rows
	var err error
	if isPostgres() {
		rows, err = b.stateDB.Query(`SELECT key, value FROM bot_state WHERE expires_at > $1`, time.Now().Unix())
	} else {
		rows, err = b.stateDB.Query(`SELECT key, value FROM bot_state WHERE expires_at > ?`, time.Now().Unix())
	}
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		stateCache.Store(k, v)
		if strings.HasPrefix(k, "img:") {
			processedImages.Store(strings.TrimPrefix(k, "img:"), true)
		} else if strings.HasPrefix(k, "txt:") {
			lastBotText.Store(strings.TrimPrefix(k, "txt:"), v)
		}
		n++
	}
	if n > 0 {
		b.addLog("info", fmt.Sprintf("state restored %d keys", n), nil)
	}
	return rows.Err()
}

func (b *bridge) GetState(key string) (string, bool) {
	v, ok := stateCache.Load(key)
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

func (b *bridge) SetState(key string, value string, ttl time.Duration) {
	stateCache.Store(key, value)
	exp := time.Now().Add(ttl).Unix()
	select {
	case b.stateWriteCh <- stateWriteJob{Key: key, Value: value, ExpiresAt: exp}:
	default:
		b.addLog("warn", "state queue penuh drop "+key, nil)
	}
}

func (b *bridge) stateWriter() {
	for job := range b.stateWriteCh {
		b.dbMu.Lock()
		if isPostgres() {
			_, _ = b.stateDB.Exec(`INSERT INTO bot_state(key, value, expires_at) VALUES($1, $2, $3) ON CONFLICT(key) DO UPDATE SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at`, job.Key, job.Value, job.ExpiresAt)
		} else {
			_, _ = b.stateDB.Exec(`INSERT OR REPLACE INTO bot_state(key, value, expires_at) VALUES(?,?,?)`, job.Key, job.Value, job.ExpiresAt)
		}
		b.dbMu.Unlock()
	}
}

func (b *bridge) startStateGC() {
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for range tick.C {
		b.dbMu.Lock()
		var res sql.Result
		var err error
		if isPostgres() {
			res, err = b.stateDB.Exec(`DELETE FROM bot_state WHERE expires_at < $1`, time.Now().Unix())
		} else {
			res, err = b.stateDB.Exec(`DELETE FROM bot_state WHERE expires_at < ?`, time.Now().Unix())
		}
		b.dbMu.Unlock()
		_ = err
		if n, _ := res.RowsAffected(); n > 0 {
			b.addLog("info", fmt.Sprintf("state GC %d expired", n), nil)
		}
	}
}
