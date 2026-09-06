package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func (b *bridge) handleMessage(evt *events.Message) {
	info := evt.Info
	isBot := false
	dir := ""
	if info.IsFromMe {
		if info.Chat.User == botJID.User || info.Chat.Server == "bot" || isBotJID(info.Chat) {
			isBot = true
			dir = "OUT"
		}
		if isBotJID(info.Sender) {
			isBot = true
			dir = "OUT"
		}
	} else {
		if info.Sender.User == botJID.User || isBotJID(info.Sender) || info.Chat.User == botJID.User || isBotJID(info.Chat) {
			isBot = true
			dir = "IN"
		}
		if info.Chat.Server == "bot" {
			isBot = true
			if dir == "" {
				dir = "IN"
			}
		}
	}
	if !isBot {
		return
	}
	rawBytes, mErr := proto.Marshal(evt.Message)
	if mErr == nil {
		for _, u := range cdnURL.FindAllString(string(rawBytes), -1) {
			b.addLog("info", "🎯 URL CDN raw: "+u, map[string]any{"id": info.ID, "src": "raw"})
		}
	}
	dump := richDump(evt.Message)
	if dump != "" {
		for _, u := range cdnURL.FindAllString(dump, -1) {
			b.addLog("info", "🎯 URL richDump: "+u, map[string]any{"id": info.ID, "src": "dump"})
		}
	}
	if pm := evt.Message.GetProtocolMessage(); pm != nil && pm.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		if ed := pm.GetEditedMessage(); ed != nil {
			rr := unwrap(ed).GetRichResponseMessage()
			if rr == nil {
				rr = ed.GetRichResponseMessage()
			}
			if rr != nil && rr.GetUnifiedResponse() != nil {
				data := rr.GetUnifiedResponse().GetData()
				if len(data) > 0 {
					var js map[string]any
					if err := json.Unmarshal(data, &js); err == nil {
						responseID, _ := js["response_id"].(string)
						if responseID != "" {
							if sections, ok := js["sections"].([]any); ok && len(sections) > 0 {
								for _, sec := range sections {
									sec0, ok := sec.(map[string]any)
									if !ok {
										continue
									}
									if vm, ok := sec0["view_model"].(map[string]any); ok {
										if prim, ok := vm["primitive"].(map[string]any); ok {
											statusStr := ""
											if st, ok := prim["status"].(map[string]any); ok {
												statusStr, _ = st["status"].(string)
											}
											if statusStr == "" || statusStr == "READY" {
												if _, loaded := processedImages.LoadOrStore(responseID, true); loaded {
												} else {
													b.SetState("img:"+responseID, "1", 24*time.Hour)
													if mediaMap, ok := prim["media"].(map[string]any); ok {
														if urlStr, ok := mediaMap["url"].(string); ok && urlStr != "" {
															mimeType, _ := mediaMap["mime_type"].(string)
															b.addLog("info", "🎯 MEDIA READY (queue): "+urlStr, map[string]any{"id": info.ID, "response_id": responseID, "mime": mimeType})
															select {
															case b.mediaJobs <- MediaJob{ResponseID: responseID, ChatID: info.Chat.String(), MsgID: info.ID, URL: urlStr, MimeType: mimeType, Kind: "cdn"}:
															default:
																processedImages.Delete(responseID)
																b.addLog("warn", "media queue penuh drop "+responseID, nil)
															}
														} else {
															raw, _ := json.Marshal(prim)
															if u2, err2 := b.extractCDNURL(string(raw), info.Chat.String()); err2 == nil && u2 != "" {
																select {
																case b.mediaJobs <- MediaJob{ResponseID: responseID, ChatID: info.Chat.String(), MsgID: info.ID, URL: u2, MimeType: "", Kind: "cdn"}:
																default:
																	processedImages.Delete(responseID)
																}
															}
														}
													} else {
														raw, _ := json.Marshal(prim)
														if u2, err2 := b.extractCDNURL(string(raw), info.Chat.String()); err2 == nil && u2 != "" {
															select {
															case b.mediaJobs <- MediaJob{ResponseID: responseID, ChatID: info.Chat.String(), MsgID: info.ID, URL: u2, MimeType: "", Kind: "cdn"}:
															default:
																processedImages.Delete(responseID)
															}
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	body := resolveEdit(evt.Message)
	_, text := extractTextFull(evt.Message, info.ID)
	if t := richText(evt.Message); t != "" {
		if text != "" {
			text += "\n" + t
		} else {
			text = t
		}
	}
	typ := kinds(body)
	if typ == "" {
		typ = protoFields(evt.Message)
		if typ == "" {
			typ = "other"
		}
	}
	respKey := ""
	if pm2 := evt.Message.GetProtocolMessage(); pm2 != nil && pm2.GetType() == waE2E.ProtocolMessage_MESSAGE_EDIT {
		if ed2 := pm2.GetEditedMessage(); ed2 != nil {
			if rr2 := unwrap(ed2).GetRichResponseMessage(); rr2 != nil && rr2.GetUnifiedResponse() != nil {
				if d2 := rr2.GetUnifiedResponse().GetData(); len(d2) > 0 {
					var js2 map[string]any
					if err := json.Unmarshal(d2, &js2); err == nil {
						if rid, ok := js2["response_id"].(string); ok {
							respKey = rid
						}
					}
				}
			}
		}
	}
	if respKey == "" {
		respKey = info.Chat.String()
	}
	if text != "" {
		if prev, ok := b.GetState("txt:" + respKey); ok {
			if text == prev {
				return
			}
			if prev != "" && strings.HasPrefix(text, prev) && len(text)-len(prev) < 40 {
				return
			}
			if prev != "" && strings.HasPrefix(prev, text) {
				return
			}
		} else if v, ok := lastBotText.Load(respKey); ok {
			if prev, ok2 := v.(string); ok2 {
				if text == prev {
					return
				}
				if prev != "" && strings.HasPrefix(text, prev) && len(text)-len(prev) < 40 {
					return
				}
				if prev != "" && strings.HasPrefix(prev, text) {
					return
				}
			}
		}
		lastBotText.Store(respKey, text)
		b.SetState("txt:"+respKey, text, 10*time.Minute)
	}
	trimTxt := text
	if trimTxt == "" {
		trimTxt = "(" + typ + ")"
	}
	if len(trimTxt) > 800 {
		trimTxt = trimTxt[:800] + "…"
	}
	var files []string
	if hasMedia(body) {
		rid := info.ID
		if _, loaded := processedImages.LoadOrStore(rid, true); !loaded {
			b.SetState("img:"+rid, "1", 24*time.Hour)
			mt := ""
			if body.GetImageMessage() != nil {
				mt = body.GetImageMessage().GetMimetype()
			} else if body.GetVideoMessage() != nil {
				mt = body.GetVideoMessage().GetMimetype()
			}
			select {
			case b.mediaJobs <- MediaJob{ResponseID: rid, ChatID: info.Chat.String(), MsgID: info.ID, MimeType: mt, Kind: "wamsg", WAMsg: body}:
			default:
				processedImages.Delete(rid)
				b.addLog("warn", "media queue penuh drop wamsg "+rid, nil)
			}
		}
	}
	for _, u := range richImages(evt.Message) {
		rid2 := evt.Info.ID + fmt.Sprintf("-%d", len(files))
		if _, loaded := processedImages.LoadOrStore(rid2, true); !loaded {
			b.SetState("img:"+rid2, "1", 24*time.Hour)
			select {
			case b.mediaJobs <- MediaJob{ResponseID: rid2, ChatID: info.Chat.String(), MsgID: evt.Info.ID, URL: u, Kind: "cdn"}:
			default:
				processedImages.Delete(rid2)
			}
		}
	}
	if text == "" && len(files) == 0 && typ == "other" {
		if evt.Message.GetProtocolMessage() == nil && richDump(evt.Message) == "" {
			return
		}
		if text == "" && len(files) == 0 {
			b.addLog("debug", fmt.Sprintf("bot msg id=%s kinds=%s fields=%s dump=%s", info.ID, typ, protoFields(evt.Message), clip(richDump(evt.Message), 200)), nil)
		}
	}
	extra := map[string]any{"dir": dir, "type": typ, "text": trimTxt, "chat": info.Chat.String(), "sender": info.Sender.String(), "id": info.ID}
	if len(files) > 0 {
		extra["media"] = files
		extra["media0"] = files[0]
	}
	msg := fmt.Sprintf("BOT %s %s [%s] %s", dir, info.Chat.String(), typ, trimTxt)
	if len(files) > 0 {
		msg += " | media: " + strings.Join(files, ",")
	}
	b.addLog("bot", msg, extra)
	if len(files) > 0 {
		b.addLog("info", "media tersimpan: "+strings.Join(files, ","), nil)
	}
}

func (b *bridge) handle(raw any) {
	switch evt := raw.(type) {
	case *events.Connected:
		b.addLog("info", "WA connected", nil)
		b.setConn(true)
	case *events.Disconnected:
		b.addLog("warn", "WA disconnected", nil)
		b.setConn(false)
	case *events.LoggedOut:
		b.addLog("warn", "WA logged out", nil)
		b.setConn(false)
	case *events.Message:
		b.handleMessage(evt)
	}
}
