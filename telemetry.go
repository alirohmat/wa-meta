package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type telemetryJob struct {
	ChatID  string
	Payload string
	Reason  string
}

func setupTelemetryTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS telemetry_failures(id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, chat_id TEXT, payload TEXT, error_reason TEXT);`)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_telemetry_ts ON telemetry_failures(timestamp DESC);`)
	return nil
}

func (b *bridge) telemetryWriter() {
	for job := range b.telemetryCh {
		b.dbMu.Lock()
		_, _ = b.stateDB.Exec(`INSERT INTO telemetry_failures(chat_id, payload, error_reason) VALUES(?,?,?)`, job.ChatID, job.Payload, job.Reason)
		b.dbMu.Unlock()
	}
}

func (b *bridge) saveTelemetryAsync(chatID, payload, reason string) {
	if len(payload) > 8000 {
		payload = payload[:8000] + "...[truncated]"
	}
	job := telemetryJob{ChatID: chatID, Payload: payload, Reason: reason}
	select {
	case b.telemetryCh <- job:
	default:
		go func() {
			b.dbMu.Lock()
			_, _ = b.stateDB.Exec(`INSERT INTO telemetry_failures(chat_id, payload, error_reason) VALUES(?,?,?)`, job.ChatID, job.Payload, job.Reason)
			b.dbMu.Unlock()
		}()
	}
}

func (b *bridge) extractCDNURL(rawPayload string, chatID string) (string, error) {
	var root any
	if err := json.Unmarshal([]byte(rawPayload), &root); err == nil {
		if u := findURLInJSON(root); u != "" {
			return u, nil
		}
	}
	if u := cdnRegex.FindString(rawPayload); u != "" {
		return u, nil
	}
	shouldSave := strings.Contains(rawPayload, "whatsapp.net") || strings.Contains(rawPayload, "mmg.") || strings.Contains(rawPayload, "media") || strings.Contains(rawPayload, "image") || strings.Contains(rawPayload, "video") || len(rawPayload) > 200
	if shouldSave {
		b.saveTelemetryAsync(chatID, rawPayload, "url not found, saved to telemetry")
	}
	return "", fmt.Errorf("url not found, saved to telemetry")
}

func findURLInJSON(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			lk := strings.ToLower(k)
			if lk == "url" || lk == "cdn_url" || lk == "media_url" || lk == "cdnurl" || lk == "image_url" || lk == "video_url" {
				if s, ok := val.(string); ok && strings.HasPrefix(s, "http") {
					return s
				}
			}
			if s := findURLInJSON(val); s != "" {
				return s
			}
		}
	case []any:
		for _, el := range x {
			if s := findURLInJSON(el); s != "" {
				return s
			}
		}
	case string:
		if strings.HasPrefix(x, "http") && cdnRegex.MatchString(x) {
			return x
		}
		if u := cdnRegex.FindString(x); u != "" {
			return u
		}
	}
	return ""
}

func (b *bridge) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case "GET":
		rows, err := b.stateDB.Query(`SELECT id, timestamp, chat_id, payload, error_reason FROM telemetry_failures ORDER BY id DESC LIMIT 10`)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id int64
			var ts, chatID, payload, reason string
			if err := rows.Scan(&id, &ts, &chatID, &payload, &reason); err != nil {
				continue
			}
			out = append(out, map[string]any{"id": id, "timestamp": ts, "chat_id": chatID, "payload": payload, "error_reason": reason})
		}
		if out == nil {
			out = []map[string]any{}
		}
		json.NewEncoder(w).Encode(out)
	case "DELETE":
		b.dbMu.Lock()
		_, err := b.stateDB.Exec(`DELETE FROM telemetry_failures`)
		b.dbMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", 405)
	}
}
