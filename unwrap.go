package main

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAICommonDeprecated"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

var containerURL = regexp.MustCompile(`container://[^\s\)\]]+`)

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
		// for container:// msgs the richResponse is inside the edited envelope — try resolveEdit
		m2 := resolveEdit(m)
		if m2 != m {
			rr = m2.GetRichResponseMessage()
		}
		if rr == nil {
			// dump raw message bytes as fallback (hunt FB CDN in raw)
			if bb, err := proto.Marshal(m); err == nil && len(bb) > 0 {
				if len(bb) > 8000 {
					bb = bb[:8000]
				}
				var b strings.Builder
				b.WriteString("raw[")
				b.WriteString(hex.EncodeToString(bb))
				b.WriteString("] txt[")
				b.WriteString(printable(bb))
				b.WriteString("]")
				return b.String()
			}
			return ""
		}
	}
	var b strings.Builder
	d := rr.GetUnifiedResponse().GetData()
	if len(d) > 0 {
		if len(d) > 8000 {
			d = d[:8000]
		}
		b.WriteString("unified[")
		b.WriteString(hex.EncodeToString(d))
		b.WriteString("] txt[")
		b.WriteString(printable(d))
		b.WriteString("] json[")
		// try pretty json for hunting FB CDN url inside unifiedResponse
		b.WriteString(clipStr(printable(d), 3000))
		b.WriteString("]")
	}
	for i, sm := range rr.GetSubmessages() {
		if i >= 5 {
			break
		}
		bb, _ := proto.Marshal(sm)
		if len(bb) > 4096 {
			bb = bb[:4096]
		}
		b.WriteString(" sub[")
		b.WriteString(hex.EncodeToString(bb))
		b.WriteString("] txt[")
		b.WriteString(printable(bb))
		b.WriteString("]")
	}
	// also append raw envelope for hunting
	if bb, err := proto.Marshal(m); err == nil && len(bb) > 0 {
		if len(bb) > 4000 {
			bb = bb[:4000]
		}
		b.WriteString(" envelope_txt[")
		b.WriteString(clipStr(printable(bb), 1500))
		b.WriteString("]")
	}
	return b.String()
}

func clipStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
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
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// renderTextCard builds a small self-contained SVG placeholder so a
// container://-only bot reply still yields a saved, viewable media file.
// ponytail: no image pipeline -> upgrade to real generator API later.
func renderTextCard(title, bodyText, ref string) []byte {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, "&", "&amp;")
		s = strings.ReplaceAll(s, "<", "&lt;")
		s = strings.ReplaceAll(s, ">", "&gt;")
		s = strings.ReplaceAll(s, `"`, "&quot;")
		return s
	}
	wrap := func(s string, width int) []string {
		var lines []string
		var cur string
		for _, w := range strings.Fields(s) {
			if len(cur)+len(w)+1 > width {
				if cur != "" {
					lines = append(lines, cur)
				}
				cur = w
			} else {
				if cur == "" {
					cur = w
				} else {
					cur += " " + w
				}
			}
			if len(lines) >= 11 {
				break
			}
		}
		if cur != "" && len(lines) < 12 {
			lines = append(lines, cur)
		}
		return lines
	}
	if len(bodyText) > 600 {
		bodyText = bodyText[:600] + "…"
	}
	lines := wrap(bodyText, 42)
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="1000" viewBox="0 0 800 1000">`)
	b.WriteString(`<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#1a1a2e"/><stop offset="1" stop-color="#16213e"/></linearGradient></defs>`)
	b.WriteString(`<rect width="800" height="1000" fill="url(#g)"/>`)
	b.WriteString(`<rect x="40" y="40" width="720" height="920" rx="24" fill="none" stroke="#4fc3f7" stroke-opacity="0.4" stroke-width="2"/>`)
	y := 130
	b.WriteString(fmt.Sprintf(`<text x="80" y="%d" font-family="system-ui,sans-serif" font-size="34" font-weight="bold" fill="#4fc3f7">%s</text>`, y, esc(title)))
	y += 50
	for _, ln := range lines {
		b.WriteString(fmt.Sprintf(`<text x="80" y="%d" font-family="system-ui,sans-serif" font-size="24" fill="#eeeeee">%s</text>`, y, esc(ln)))
		y += 38
	}
	y += 30
	b.WriteString(fmt.Sprintf(`<text x="80" y="%d" font-family="monospace" font-size="16" fill="#888888">ref: %s</text>`, 920, esc(ref)))
	b.WriteString(`</svg>`)
	return []byte(b.String())
}
func containerRefs(m *waE2E.Message) []string {
	t := richText(unwrap(m))
	if t == "" {
		return nil
	}
	found := containerURL.FindAllString(t, -1)
	seen := map[string]struct{}{}
	var out []string
	for _, u := range found {
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}
func hasWABinary(m *waE2E.Message) bool {
	m = unwrap(m)
	if m.GetImageMessage() != nil || m.GetVideoMessage() != nil || m.GetAudioMessage() != nil || m.GetDocumentMessage() != nil || m.GetStickerMessage() != nil {
		return true
	}
	dl, _ := interactiveMedia(m)
	return dl != nil
}
func hasMedia(m *waE2E.Message) bool {
	if hasWABinary(m) {
		return true
	}
	// ponytail: rich submessages carry text refs, not WA binary -> skip wamsg job
	return len(richImages(m)) > 0
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
