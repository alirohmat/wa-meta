package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

var cdnURL = regexp.MustCompile(`https?://[^ ]+?[.](?:jpg|jpeg|png|webp|mp4|mov|webm)(?:[?][^ ]*)?`)
var cdnRegex = cdnURL
var processedImages sync.Map
var lastBotText sync.Map

type MediaJob struct {
	ResponseID string
	ChatID     string
	MsgID      string
	URL        string
	MimeType   string
	Kind       string
	WAMsg      *waE2E.Message
}

func (b *bridge) fetchAndSaveCDN(ctx context.Context, job MediaJob) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", job.URL, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("cdn status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return 0, "", err
	}
	ext := ".jpg"
	mt := strings.ToLower(job.MimeType)
	switch {
	case strings.Contains(mt, "png"):
		ext = ".png"
	case strings.Contains(mt, "webp"):
		ext = ".webp"
	case strings.Contains(mt, "mp4"):
		ext = ".mp4"
	case strings.Contains(mt, "mov"):
		ext = ".mov"
	case strings.Contains(mt, "jpeg"), strings.Contains(mt, "jpg"):
		ext = ".jpg"
	}
	pub, err := b.saveBytesToCAS(data, ext, job.ChatID, job.MsgID)
	if err != nil {
		return 0, "", err
	}
	b.SetState("imgpub:"+job.ResponseID, pub, 24*time.Hour)
	b.hook(map[string]any{"kind": "media_ready", "url": job.URL, "file": pub, "response_id": job.ResponseID, "bytes": len(data)})
	b.addLog("bot", fmt.Sprintf("BOT IN %s [media READY] %s (%d bytes) %s", job.ChatID, job.URL, len(data), pub), map[string]any{"dir": "IN", "type": "media", "text": job.URL, "media": pub, "response_id": job.ResponseID})
	return len(data), pub, nil

}

func (b *bridge) downloadAndSaveWAMsg(ctx context.Context, job MediaJob) (int, string, error) {
	type res struct {
		data []byte
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		var d []byte
		var e error
		m := job.WAMsg
		if m.GetImageMessage() != nil {
			d, e = b.client.Download(ctx, m.GetImageMessage())
		} else if m.GetVideoMessage() != nil {
			d, e = b.client.Download(ctx, m.GetVideoMessage())
		} else if m.GetAudioMessage() != nil {
			d, e = b.client.Download(ctx, m.GetAudioMessage())
		} else if m.GetDocumentMessage() != nil {
			d, e = b.client.Download(ctx, m.GetDocumentMessage())
		} else if m.GetStickerMessage() != nil {
			d, e = b.client.Download(ctx, m.GetStickerMessage())
		} else {
			if dl, _ := interactiveMedia(m); dl != nil {
				d, e = b.client.Download(ctx, dl)
			} else {
				e = fmt.Errorf("no downloadable media")
			}
		}
		ch <- res{d, e}
	}()
	var data []byte
	select {
	case <-ctx.Done():
		return 0, "", ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return 0, "", r.err
		}
		data = r.data
	}
	ext := mimeToExt(job.MimeType)
	if ext == "" {
		ext = ".jpg"
	}
	pub, err := b.saveBytesToCAS(data, ext, job.ChatID, job.MsgID)
	if err != nil {
		return 0, "", err
	}
	b.SetState("imgpub:"+job.ResponseID, pub, 24*time.Hour)
	b.hook(map[string]any{"kind": "media_ready", "url": "", "file": pub, "msg_id": job.MsgID, "bytes": len(data)})
	b.addLog("bot", fmt.Sprintf("BOT %s %s [media] (%d bytes) %s", "OUT", job.ChatID, len(data), pub), map[string]any{"dir": "OUT", "media": pub, "chat": job.ChatID, "id": job.MsgID})
	return len(data), pub, nil

}

func wantsImage(prompt string) bool {
	s := strings.ToLower(prompt)
	for _, k := range []string{"gambar", "image", "foto", "photo", "picture", "lukis", "draw", "buatkan", "buatin", "generate image", "sketsa"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func pollinationsURL(prompt string, width, height, seed int) string {
	if width <= 0 {
		width = 768
	}
	if height <= 0 {
		height = 768
	}
	q := url.QueryEscape(strings.TrimSpace(prompt))
	if q == "" {
		q = "random"
	}
	return fmt.Sprintf("https://image.pollinations.ai/prompt/%s?width=%d&height=%d&seed=%d&nologo=true", q, width, height, seed)
}

func detectImageExt(data []byte, contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case strings.Contains(ct, "gif"):
		return ".gif"
	}
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return ".png"
	}
	if len(data) >= 12 && string(data[8:12]) == "WEBP" {
		return ".webp"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return ".jpg"
	}
	return ".jpg"
}

func (b *bridge) fetchPollinations(ctx context.Context, prompt, chatID, msgID string) (string, int, error) {
	seed := int(time.Now().UnixNano() % 1000000)
	u := pollinationsURL(prompt, 768, 768, seed)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "wabot/1.0")
	req.Header.Set("Accept", "image/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("pollinations status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 15<<20))
	if err != nil {
		return "", 0, err
	}
	if len(data) < 1024 {
		return "", 0, fmt.Errorf("pollinations body too small: %d", len(data))
	}
	ext := detectImageExt(data, resp.Header.Get("Content-Type"))
	pub, err := b.saveBytesToCAS(data, ext, chatID, msgID)
	if err != nil {
		return "", 0, err
	}
	b.SetState("imgpub:pollinations:"+msgID, pub, 24*time.Hour)
	b.hook(map[string]any{"kind": "media_ready", "url": u, "file": pub, "response_id": "pollinations:" + msgID, "bytes": len(data), "source": "pollinations"})
	b.addLog("bot", fmt.Sprintf("BOT IN %s [media POLLINATIONS] %s (%d bytes) %s", chatID, u, len(data), pub), map[string]any{"dir": "IN", "type": "media", "text": u, "media": pub, "response_id": "pollinations:" + msgID, "source": "pollinations"})
	return pub, len(data), nil
}

func (b *bridge) saveTextCard(ctx context.Context, job MediaJob) (int, string, error) {
	data := renderTextCard("Meta AI", job.MimeType, job.URL)
	if len(data) == 0 {
		return 0, "", fmt.Errorf("render kosong")
	}
	pub, err := b.saveBytesToCAS(data, ".svg", job.ChatID, job.MsgID)
	if err != nil {
		return 0, "", err
	}
	b.SetState("imgpub:"+job.ResponseID, pub, 24*time.Hour)
	b.hook(map[string]any{"kind": "media_ready", "url": job.URL, "file": pub, "response_id": job.ResponseID, "bytes": len(data), "fallback": "text-card"})
	b.addLog("bot", fmt.Sprintf("BOT IN %s [media CARD] %s (%d bytes) %s", job.ChatID, job.URL, len(data), pub), map[string]any{"dir": "IN", "type": "media", "text": job.URL, "media": pub, "response_id": job.ResponseID})
	return len(data), pub, nil
}

func (b *bridge) mediaWorker() {
	for job := range b.mediaJobs {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		var err error
		if job.Kind == "cdn" {
			_, _, err = b.fetchAndSaveCDN(ctx, job)
		} else if job.Kind == "card" {
			_, _, err = b.saveTextCard(ctx, job)
		} else {
			_, _, err = b.downloadAndSaveWAMsg(ctx, job)
		}
		cancel()
		if err != nil {
			if err.Error() == "no downloadable media" {
				b.addLog("debug", "media job skip no downloadable media "+job.ResponseID, nil)
			} else {
				b.addLog("warn", "media job gagal "+job.ResponseID+": "+err.Error(), nil)
			}
			processedImages.Delete(job.ResponseID)
			continue
		}
	}
}

func (b *bridge) fetchURL(id, u string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.addLog("warn", "fetch gambar gagal: "+err.Error(), nil)
		return ""
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil || len(data) == 0 {
		b.addLog("warn", fmt.Sprintf("fetch gambar kosong: %v", err), nil)
		return ""
	}
	ext := ".jpg"
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "png") {
		ext = ".png"
	} else if strings.Contains(ct, "webp") {
		ext = ".webp"
	}
	os.MkdirAll(mediaDir, 0755)
	name := fmt.Sprintf("%s-ai%s", id, ext)
	os.WriteFile(filepath.Join(mediaDir, name), data, 0644)
	return publicPrefix + "/" + name
}
func (b *bridge) download(msgID string, m *waE2E.Message, dlCtx context.Context) string {
	ctx, cancel := context.WithTimeout(dlCtx, 60*time.Second)
	defer cancel()
	var data []byte
	var ext string
	var err error
	m = unwrap(m)
	dl, dlExt := interactiveMedia(m)
	switch {
	case m.GetImageMessage() != nil:
		data, err = b.client.Download(ctx, m.GetImageMessage())
		ext = ".jpg"
	case m.GetVideoMessage() != nil:
		data, err = b.client.Download(ctx, m.GetVideoMessage())
		ext = ".mp4"
	case m.GetAudioMessage() != nil:
		data, err = b.client.Download(ctx, m.GetAudioMessage())
		ext = ".ogg"
	case m.GetDocumentMessage() != nil:
		data, err = b.client.Download(ctx, m.GetDocumentMessage())
		ext = filepath.Ext(m.GetDocumentMessage().GetFileName())
		if ext == "" {
			ext = ".bin"
		}
	case m.GetStickerMessage() != nil:
		data, err = b.client.Download(ctx, m.GetStickerMessage())
		ext = ".webp"
	default:
		if dl == nil {
			return ""
		}
		data, err = b.client.Download(ctx, dl)
		ext = dlExt
	}
	if err != nil || len(data) == 0 {
		b.addLog("warn", fmt.Sprintf("download gagal: %v", err), nil)
		return ""
	}
	os.MkdirAll(mediaDir, 0755)
	name := fmt.Sprintf("%s%s", msgID, ext)
	os.WriteFile(filepath.Join(mediaDir, name), data, 0644)
	return publicPrefix + "/" + name
}
