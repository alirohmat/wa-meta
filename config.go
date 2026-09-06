package main

import (
	"net/http"
	"os"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

func envStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// getDSN resolves the database driver+DSN from environment. If any of
// DATABASE_URL / POSTGRES_URL / POSTGRES_DSN is set, postgres via pgx is
// selected; otherwise the legacy embedded sqlite3 file is used.
func getDSN() (string, string) {
	for _, env := range []string{"DATABASE_URL", "POSTGRES_URL", "POSTGRES_DSN"} {
		if dsn := strings.TrimSpace(os.Getenv(env)); dsn != "" {
			return "pgx", dsn
		}
	}
	return "sqlite3", "file:wabot.db?_foreign_keys=on"
}

// isPostgres reports whether the current driver selection is postgres/pgx.
func isPostgres() bool {
	driver, _ := getDSN()
	return driver == "pgx"
}

var botJID = parseBot(envStr("BOT", "867051314767696@bot"), types.NewJID("867051314767696", "bot"))
var webhook = os.Getenv("WEBHOOK")
var mediaDir = envStr("MEDIA_DIR", "./media")
var publicPrefix = envStr("PUBLIC_PREFIX", "/media")
var apiKey = func() string {
	if v := strings.TrimSpace(os.Getenv("API_KEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("WABOT_API_KEY"))
}()

func checkAPIKey(r *http.Request) bool {
	if apiKey == "" {
		return true
	}
	if v := strings.TrimSpace(r.Header.Get("X-API-Key")); v != "" && v == apiKey {
		return true
	}
	if a := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(a), "bearer ") && strings.TrimSpace(a[7:]) == apiKey {
		return true
	}
	if q := strings.TrimSpace(r.URL.Query().Get("key")); q != "" && q == apiKey {
		return true
	}
	return false
}

func requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkAPIKey(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"ok":false,"error":"unauthorized: API key required (X-API-Key or Authorization: Bearer)"}`))
			return
		}
		next(w, r)
	}
}

func parseBot(s string, def types.JID) types.JID {
	if j, err := types.ParseJID(s); err == nil && j.User != "" {
		return j
	}
	parts := strings.SplitN(s, "@", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] == "bot" {
		return types.NewJID(parts[0], "bot")
	}
	return def
}
func isBotJID(j types.JID) bool { return j.Server == "bot" }

func getExtension(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpeg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	default:
		return ".bin"
	}
}

func mimeToExt(mime string) string {
	m := strings.ToLower(mime)
	switch {
	case strings.Contains(m, "jpeg"), strings.Contains(m, "jpg"):
		return ".jpg"
	case strings.Contains(m, "png"):
		return ".png"
	case strings.Contains(m, "webp"):
		return ".webp"
	case strings.Contains(m, "mp4"):
		return ".mp4"
	case strings.Contains(m, "mov"):
		return ".mov"
	case strings.Contains(m, "webm"):
		return ".webm"
	default:
		return ""
	}
}
