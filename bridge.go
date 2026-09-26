package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	reconnectMu  sync.Mutex
	jobsMu       sync.RWMutex
	jobs         map[string]*generateJob
}

type generateJob struct {
	ID        string    `json:"job_id"`
	Status    string    `json:"status"`
	To        string    `json:"to"`
	Text      string    `json:"text"`
	Reply     string    `json:"reply,omitempty"`
	Media     []string  `json:"media,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Error     string    `json:"error,omitempty"`
	WantMedia bool      `json:"want_media"`
}

func (b *bridge) putJob(j *generateJob) {
	b.jobsMu.Lock()
	if b.jobs == nil {
		b.jobs = make(map[string]*generateJob)
	}
	b.jobs[j.ID] = j
	b.jobsMu.Unlock()
}

func (b *bridge) getJob(id string) *generateJob {
	b.jobsMu.RLock()
	j := b.jobs[id]
	if j != nil {
		copy := *j
		copy.Media = append([]string(nil), j.Media...)
		j = &copy
	}
	b.jobsMu.RUnlock()
	return j
}

func (b *bridge) watchGenerateJob(id, target string, sentAt time.Time, snapN int) {
	deadline := time.Now().Add(15 * time.Minute)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for time.Now().Before(deadline) {
		reply := ""
		all := b.logs.all()
		start := snapN
		if start > len(all) {
			start = len(all)
		}
		for _, e := range all[start:] {
			if e.Level != "bot" {
				continue
			}
			ex, ok := e.Extra.(map[string]any)
			if !ok || ex["dir"] != "IN" || ex["chat"] != target {
				continue
			}
			if text, ok := ex["text"].(string); ok && strings.TrimSpace(text) != "" {
				reply = text
			}
		}
		media := []string{}
		_ = filepath.WalkDir(mediaDir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.ModTime().Before(sentAt) {
				return nil
			}
			rel, _ := filepath.Rel(mediaDir, p)
			media = append(media, publicPrefix+"/"+filepath.ToSlash(rel))
			return nil
		})
		sort.Strings(media)
		b.jobsMu.Lock()
		j := b.jobs[id]
		if j == nil {
			b.jobsMu.Unlock()
			return
		}
		j.Reply, j.Media, j.UpdatedAt = reply, media, time.Now()
		wantMedia := j.WantMedia
		if reply != "" && (!wantMedia || len(media) > 0) {
			j.Status = "completed"
		} else if reply != "" {
			j.Status = "processing_media"
		} else {
			j.Status = "processing"
		}
		b.jobsMu.Unlock()
		if reply != "" && (!wantMedia || len(media) > 0) {
			return
		}
		<-tick.C
	}
	b.jobsMu.Lock()
	if j := b.jobs[id]; j != nil {
		j.Status = "timeout"
		j.Error = "balasan Meta belum lengkap setelah 15 menit"
		j.UpdatedAt = time.Now()
	}
	b.jobsMu.Unlock()
}

func (b *bridge) connectWithRetry() error {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		if b.client.IsConnected() {
			return nil
		}
		if err := b.client.Connect(); err == nil {
			return nil
		} else {
			lastErr = err
			b.addLog("warn", fmt.Sprintf("WA connect gagal attempt=%d: %v", attempt+1, err), nil)
		}
		time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
	}
	return lastErr
}

func (b *bridge) reconnect() {
	b.reconnectMu.Lock()
	defer b.reconnectMu.Unlock()
	if b.client.Store.ID == nil || b.client.IsConnected() {
		return
	}
	b.addLog("info", "WA reconnect dimulai", nil)
	if err := b.client.Connect(); err != nil {
		b.addLog("warn", "WA reconnect gagal: "+err.Error(), nil)
		return
	}
	b.addLog("info", "WA reconnect berhasil", nil)
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
