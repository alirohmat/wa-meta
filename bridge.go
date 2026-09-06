package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

type bridge struct {
	client       *whatsmeow.Client
	container    *sqlstore.Container
	logs         *logStore
	pm           sync.RWMutex
	phone        string
	pairCode     string
	pairExp      time.Time
	connected    bool
	pairMu       sync.Mutex
	mediaJobs    chan MediaJob
	stateDB      *sql.DB
	stateWriteCh chan stateWriteJob
	telemetryCh  chan telemetryJob
	dbMu         sync.Mutex
}

func (b *bridge) setConn(v bool) {
	b.pm.Lock()
	b.connected = v
	b.pm.Unlock()
	rl, mins := rateStatus()
	st := map[string]any{"connected": v, "logged": b.client.Store.ID != nil}
	if rl {
		st["rate_limited"] = true
		st["retry_after_minutes"] = mins
		st["message"] = fmt.Sprintf("WA rate limit, tunggu %d menit lagi", mins)
	}
	sse.send("state", st)
}
func (b *bridge) hook(payload any) {
	if webhook == "" {
		return
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", webhook, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	go func() {
		c := &http.Client{Timeout: 5 * time.Second}
		resp, err := c.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
}
func (b *bridge) addLog(level, msg string, extra any) {
	e := b.logs.add(level, msg, extra)
	if level == "bot" {
		sse.send("bot", e)
	} else {
		sse.send("log", e)
	}
	b.hook(map[string]any{"kind": "log", "t": e.T, "level": level, "msg": msg, "extra": extra})
}
func (b *bridge) requestPair(ctx context.Context, phone string) (string, error) {
	b.pairMu.Lock()
	defer b.pairMu.Unlock()
	b.pm.RLock()
	prevPhone := b.phone
	prevCode := b.pairCode
	prevExp := b.pairExp
	b.pm.RUnlock()
	if prevPhone == phone && prevCode != "" && time.Now().Before(prevExp) {
		b.addLog("info", "reuse code masih hidup: "+prevCode, nil)
		return prevCode, nil
	}
	if !b.client.IsConnected() {
		time.Sleep(1100 * time.Millisecond)
	} else {
		time.Sleep(300 * time.Millisecond)
	}
	code, err := b.client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (MacOS)")
	if err != nil {
		return "", err
	}
	b.pm.Lock()
	b.phone = phone
	b.pairCode = code
	b.pairExp = time.Now().Add(60 * time.Second)
	exp := b.pairExp
	b.pm.Unlock()
	b.addLog("info", "pairing code "+phone+": "+code, nil)
	sse.send("pair", map[string]any{"phone": phone, "code": code, "exp": exp.Format(time.RFC3339)})
	return code, nil
}
