package main

import (
	"encoding/hex"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAICommonDeprecated"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func unwrap(m *waE2E.Message) *waE2E.Message {
	for m != nil {
		var inner *waE2E.Message
		switch {
		case m.GetDeviceSentMessage() != nil:
			inner = m.GetDeviceSentMessage().GetMessage()
		case m.GetEphemeralMessage() != nil:
			inner = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage() != nil:
			inner = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2() != nil:
			inner = m.GetViewOnceMessageV2().GetMessage()
		case m.GetDocumentWithCaptionMessage() != nil:
			inner = m.GetDocumentWithCaptionMessage().GetMessage()
		case m.GetBotInvokeMessage() != nil:
			inner = m.GetBotInvokeMessage().GetMessage()
		case m.GetBotTaskMessage() != nil:
			inner = m.GetBotTaskMessage().GetMessage()
		}
		if inner == nil {
			return m
		}
		m = inner
	}
	return m
}
func interactiveMedia(m *waE2E.Message) (whatsmeow.DownloadableMessage, string) {
	im := m.GetInteractiveMessage()
	if im == nil {
		return nil, ""
	}
	h := im.GetHeader()
	if h == nil {
		return nil, ""
	}
	if v := h.GetVideoMessage(); v != nil {
		return v, ".mp4"
	}
	if v := h.GetImageMessage(); v != nil {
		return v, ".jpg"
	}
	if v := h.GetDocumentMessage(); v != nil {
		return v, ".bin"
	}
	return nil, ""
}
func interactiveText(m *waE2E.Message) string {
	im := m.GetInteractiveMessage()
	if im == nil {
		return ""
	}
	if t := im.GetBody().GetText(); t != "" {
		return t
	}
	h := im.GetHeader()
	if h == nil {
		return ""
	}
	if t := h.GetTitle(); t != "" {
		if s := h.GetSubtitle(); s != "" {
			return t + "\n" + s
		}
		return t
	}
	return h.GetSubtitle()
}
func richImages(m *waE2E.Message) []string {
	rr := unwrap(m).GetRichResponseMessage()
	if rr == nil {
		return nil
	}
	var out []string
	push := func(u *waAICommonDeprecated.AIRichResponseImageURL) {
		if u == nil {
			return
		}
		if s := u.GetImageHighResURL(); s != "" {
			out = append(out, s)
		} else if s := u.GetImagePreviewURL(); s != "" {
			out = append(out, s)
		} else if s := u.GetSourceURL(); s != "" {
			out = append(out, s)
		}
	}
	for _, sm := range rr.GetSubmessages() {
		if sm == nil {
			continue
		}
		if g := sm.GetGridImageMetadata(); g != nil {
			push(g.GetGridImageURL())
			for _, u := range g.GetImageURLs() {
				push(u)
			}
		}
		push(sm.GetImageMetadata().GetImageURL())
	}
	return out
}
func richDump(m *waE2E.Message) string {
	rr := unwrap(m).GetRichResponseMessage()
	if rr == nil {
		return ""
	}
	var b strings.Builder
	if d := rr.GetUnifiedResponse().GetData(); len(d) > 0 {
		if len(d) > 2048 {
			d = d[:2048]
		}
		b.WriteString("unified[")
		b.WriteString(hex.EncodeToString(d))
		b.WriteString("] txt[")
		b.WriteString(printable(d))
		b.WriteString("]")
	}
	for i, sm := range rr.GetSubmessages() {
		if i >= 5 {
			break
		}
		bb, _ := proto.Marshal(sm)
		if len(bb) > 1024 {
			bb = bb[:1024]
		}
		b.WriteString(" sub[")
		b.WriteString(hex.EncodeToString(bb))
		b.WriteString("] txt[")
		b.WriteString(printable(bb))
		b.WriteString("]")
	}
	return b.String()
}
func printable(d []byte) string {
	var b strings.Builder
	for _, c := range d {
		if c >= 32 && c < 127 {
			b.WriteByte(c)
		} else if c == 10 || c == 13 || c == 9 {
			b.WriteByte(' ')
		}
	}
	s := b.String()
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
func richText(m *waE2E.Message) string {
	rr := unwrap(m).GetRichResponseMessage()
	if rr == nil {
		return ""
	}
	var b strings.Builder
	for _, sm := range rr.GetSubmessages() {
		if t := sm.GetMessageText(); t != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(t)
		}
	}
	return b.String()
}
func extractTextFull(m *waE2E.Message, fallbackID string) (string, string) {
	m = unwrap(m)
	id := fallbackID
	if pm := m.GetProtocolMessage(); pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		if k := pm.GetKey().GetID(); k != "" {
			id = k
		}
		if ed := unwrap(pm.GetEditedMessage()); ed != nil {
			if t := ed.GetConversation(); t != "" {
				return id, t
			}
			if t := ed.GetExtendedTextMessage().GetText(); t != "" {
				return id, t
			}
			if t := interactiveText(ed); t != "" {
				return id, t
			}
			if t := richText(ed); t != "" {
				return id, t
			}
		}
	}
	if t := m.GetConversation(); t != "" {
		return id, t
	}
	if t := m.GetExtendedTextMessage().GetText(); t != "" {
		return id, t
	}
	if t := interactiveText(m); t != "" {
		return id, t
	}
	if t := richText(m); t != "" {
		return id, t
	}
	return id, ""
}
func hasMedia(m *waE2E.Message) bool {
	m = unwrap(m)
	if m.GetImageMessage() != nil || m.GetVideoMessage() != nil || m.GetAudioMessage() != nil || m.GetDocumentMessage() != nil || m.GetStickerMessage() != nil {
		return true
	}
	if m.GetRichResponseMessage() != nil {
		return true
	}
	dl, _ := interactiveMedia(m)
	return dl != nil
}
func kinds(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	m = unwrap(m)
	var k []string
	if m.GetConversation() != "" || m.GetExtendedTextMessage() != nil {
		k = append(k, "text")
	}
	if m.GetImageMessage() != nil {
		k = append(k, "image")
	}
	if m.GetVideoMessage() != nil {
		k = append(k, "video")
	}
	if m.GetAudioMessage() != nil {
		k = append(k, "audio")
	}
	if m.GetDocumentMessage() != nil {
		k = append(k, "doc")
	}
	if m.GetStickerMessage() != nil {
		k = append(k, "sticker")
	}
	if m.GetInteractiveMessage() != nil {
		k = append(k, "interactive")
	}
	if m.GetAlbumMessage() != nil {
		k = append(k, fmt.Sprintf("album:%dimg+%dvid", m.GetAlbumMessage().GetExpectedImageCount(), m.GetAlbumMessage().GetExpectedVideoCount()))
	}
	if m.GetRichResponseMessage() != nil {
		k = append(k, fmt.Sprintf("rich:%dsub", len(m.GetRichResponseMessage().GetSubmessages())))
	}
	if pm := m.GetProtocolMessage(); pm != nil {
		k = append(k, "protocol:"+pm.GetType().String())
	}
	if len(k) == 0 {
		k = append(k, "other")
	}
	return strings.Join(k, ",")
}
func protoFields(m proto.Message) string {
	if m == nil {
		return ""
	}
	r := m.ProtoReflect()
	if r == nil {
		return ""
	}
	fd := r.Descriptor().Fields()
	var out []string
	for i := 0; i < fd.Len(); i++ {
		f := fd.Get(i)
		if r.Has(f) {
			out = append(out, string(f.Name()))
		}
	}
	return strings.Join(out, "|")
}
func resolveEdit(m *waE2E.Message) *waE2E.Message {
	if pm := m.GetProtocolMessage(); pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		if ed := unwrap(pm.GetEditedMessage()); ed != nil {
			return ed
		}
	}
	return unwrap(m)
}

func extractText(m *waE2E.Message) string {
	_, t := extractTextFull(m, "")
	return t
}
func describeMsg(m *waE2E.Message) string { return kinds(unwrap(m)) }

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
