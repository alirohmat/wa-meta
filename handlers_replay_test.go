package main

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAICommonDeprecated"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func testBridge() *bridge {
	return &bridge{
		logs:         &logStore{},
		mediaJobs:    make(chan MediaJob, 64),
		stateWriteCh: make(chan stateWriteJob, 256),
	}
}

func botMsgEvent(id, text string) *events.Message {
	bot := types.NewJID(botJID.User, "bot")
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     bot,
				Sender:   bot,
				IsFromMe: false,
			},
			ID: types.MessageID(id),
		},
		Message: &waE2E.Message{
			RichResponseMessage: &waE2E.AIRichResponseMessage{
				Submessages: []*waAICommonDeprecated.AIRichResponseSubMessage{
					{MessageText: &text},
				},
			},
		},
	}
}

// Replay of the production failure: bot sends rich:1sub text with a
// container:// ref. No WA binary exists, so the handler queues a text-card
// fallback job (rendered SVG) instead of a doomed wamsg download.
func TestHandleMessageContainerRefNoJob(t *testing.T) {
	b := testBridge()
	text := "Here is your scene 1:\n\n![image](container:///mnt/data/medina_dawn.webp)\n\nCourtyard at dawn"
	b.handleMessage(botMsgEvent("replay-1", text))

	if len(b.mediaJobs) != 1 {
		t.Fatalf("mediaJobs = %d, want 1 text-card fallback job", len(b.mediaJobs))
	}
	job := <-b.mediaJobs
	if job.Kind != "card" || job.URL != "container:///mnt/data/medina_dawn.webp" {
		t.Fatalf("job = %+v, want card container:///mnt/data/medina_dawn.webp", job)
	}
	found := false
	for _, e := range b.logs.all() {
		if strings.Contains(e.Msg, "media ref") && strings.Contains(e.Msg, "container:///mnt/data/medina_dawn.webp") {
			found = true
		}
		if strings.Contains(e.Msg, "no downloadable media") {
			t.Fatalf("unexpected skip log still present: %s", e.Msg)
		}
	}
	if !found {
		t.Fatal("want 🖼️ media ref log for container:// URL, got none")
	}
}

// Same replay with a real rich image URL must still queue a CDN job.
func TestHandleMessageRichURLQueuesCDN(t *testing.T) {
	b := testBridge()
	m := botMsgEvent("replay-2", "text only")
	sub := m.Message.RichResponseMessage.Submessages[0]
	sub.ImageMetadata = &waAICommonDeprecated.AIRichResponseInlineImageMetadata{
		ImageURL: &waAICommonDeprecated.AIRichResponseImageURL{
			ImageHighResURL: strp("https://example.com/b.webp"),
		},
	}
	b.handleMessage(m)

	if len(b.mediaJobs) != 1 {
		t.Fatalf("mediaJobs = %d, want 1 CDN job", len(b.mediaJobs))
	}
	job := <-b.mediaJobs
	if job.Kind != "cdn" || job.URL != "https://example.com/b.webp" {
		t.Fatalf("job = %+v, want cdn https://example.com/b.webp", job)
	}
}

// Production sends the bot reply as MESSAGE_EDIT wrapping the rich payload.
// The container ref must still surface (unwrap before scan).
func TestHandleMessageEditContainerRef(t *testing.T) {
	b := testBridge()
	inner := botMsgEvent("replay-edit", "Ini oyen:\n\n![image](container:///mnt/data/image.webp)\n\nLucu")
	m := &events.Message{
		Info: inner.Info,
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
				Key:           &waCommon.MessageKey{ID: strp("replay-edit")},
				EditedMessage: inner.Message,
			},
		},
	}
	b.handleMessage(m)

	if len(b.mediaJobs) != 1 {
		t.Fatalf("mediaJobs = %d, want 1 card job for edit-wrapped container ref", len(b.mediaJobs))
	}
	job := <-b.mediaJobs
	if job.Kind != "card" {
		t.Fatalf("job.Kind = %s, want card", job.Kind)
	}
	found := false
	for _, e := range b.logs.all() {
		if strings.Contains(e.Msg, "media ref") && strings.Contains(e.Msg, "container:///mnt/data/image.webp") {
			found = true
		}
		if strings.Contains(e.Msg, "no downloadable media") {
			t.Fatalf("unexpected skip log still present: %s", e.Msg)
		}
	}
	if !found {
		t.Fatal("want 🖼️ media ref log for edit-wrapped container:// URL, got none")
	}
}

// Three streaming edits for the same original message must log the
// container ref only once (dedupe by edit key), not once per edit.
func TestHandleMessageEditContainerRefDeduped(t *testing.T) {
	b := testBridge()
	pad := strings.Repeat("x", 60)
	for i := 0; i < 3; i++ {
		inner := botMsgEvent("replay-dedup-outer", "Oyen part "+string(rune('A'+i))+" "+pad+"\n\n![image](container:///mnt/data/image.webp)\n\n"+pad)
		m := &events.Message{
			Info: inner.Info,
			Message: &waE2E.Message{
				ProtocolMessage: &waE2E.ProtocolMessage{
					Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
					Key:           &waCommon.MessageKey{ID: strp("replay-dedup")},
					EditedMessage: inner.Message,
				},
			},
		}
		b.handleMessage(m)
	}
	n := 0
	for _, e := range b.logs.all() {
		if strings.Contains(e.Msg, "media ref") && strings.Contains(e.Msg, "container:///mnt/data/image.webp") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("media ref logs = %d, want exactly 1 across 3 streaming edits", n)
	}
}
