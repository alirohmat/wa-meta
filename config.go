package main

import (
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

var botJID = parseBot(envStr("BOT", "867051314767696@bot"), types.NewJID("867051314767696", "bot"))
var webhook = os.Getenv("WEBHOOK")
var mediaDir = envStr("MEDIA_DIR", "./media")
var publicPrefix = envStr("PUBLIC_PREFIX", "/media")

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
