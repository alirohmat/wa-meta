package main

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waAICommonDeprecated"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

func strp(s string) *string { return &s }

func TestContainerRefs(t *testing.T) {
	m := &waE2E.Message{
		RichResponseMessage: &waE2E.AIRichResponseMessage{
			Submessages: []*waAICommonDeprecated.AIRichResponseSubMessage{
				{MessageText: strp("see ![image](container:///mnt/data/foo.webp) done")},
				{MessageText: strp("dup ![image](container:///mnt/data/foo.webp)")},
			},
		},
	}
	got := containerRefs(m)
	if len(got) != 1 || got[0] != "container:///mnt/data/foo.webp" {
		t.Fatalf("containerRefs = %v, want single deduped ref", got)
	}
	if hasMedia(m) {
		t.Fatal("hasMedia = true for text-only rich refs, want false (no WA binary)")
	}
}

func TestHasMediaRichBinary(t *testing.T) {
	m := &waE2E.Message{
		RichResponseMessage: &waE2E.AIRichResponseMessage{
			Submessages: []*waAICommonDeprecated.AIRichResponseSubMessage{
				{
					ImageMetadata: &waAICommonDeprecated.AIRichResponseInlineImageMetadata{
						ImageURL: &waAICommonDeprecated.AIRichResponseImageURL{
							ImageHighResURL: strp("https://example.com/a.webp"),
						},
					},
				},
			},
		},
	}
	if !hasMedia(m) {
		t.Fatal("hasMedia = false for richImages URL, want true")
	}
	if got := richImages(m); len(got) != 1 || got[0] != "https://example.com/a.webp" {
		t.Fatalf("richImages = %v", got)
	}
}

func TestHasMediaWABinary(t *testing.T) {
	m := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: strp("image/jpeg")}}
	if !hasMedia(m) {
		t.Fatal("hasMedia = false for ImageMessage, want true")
	}
}
