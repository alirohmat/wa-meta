// wabot — pairing + live @bot IN/OUT + media download (whatsmeow + sqlite)
// Web: pairing XXXX-XXXX + SSE live bot log warp auto-scroll + preview media
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const page = `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1">
<title>wabot — pairing + live @bot</title>
<style>
body{font-family:system-ui;background:#111;color:#eee;max-width:720px;margin:auto;padding:16px}
input{padding:8px}button{padding:10px 14px;cursor:pointer}
#code{font-size:28px;letter-spacing:4px;color:#0f0;margin:12px 0}
pre{white-space:pre-wrap;word-break:break-word;overflow-wrap:anywhere;background:#222;padding:10px;max-height:320px;overflow:auto;border:1px solid #333;border-radius:6px}
#botLog{background:#1a1a2e;border-color:#444;max-height:520px}
.bot-in{color:#4fc3f7}.bot-out{color:#ffb74d}
small{color:#999}
#gallery img,#gallery video{max-width:100%;border-radius:6px;margin:4px 0}
</style>
<h1>WA Pairing</h1>
<div id=rateBanner style="display:none;background:#4a1a1a;border:1px solid #f44336;border-radius:8px;padding:12px;margin-bottom:12px;font-weight:bold"></div>
<input id=phone placeholder="628xxx" style="width:200px">
<button onclick="pair()">Minta code</button>
<button onclick="logout()" style="margin-left:6px;background:#f44336;color:#fff;border:none;border-radius:6px">Log out</button>
<div id=code></div>
<div><small>WA > Perangkat tertaut > Tautkan dengan nomor telepon. Code 60 detik (XXXX-XXXX pakai strip).</small></div>
<div id=conn style="margin:8px 0;color:#aaa"></div>

<h3>Live @bot — pesan masuk / keluar</h3>
<div style="display:flex;gap:6px;margin:6px 0;flex-wrap:wrap">
<select id=botJid style="width:320px;padding:8px" title="target JID"><option value="867051314767696@bot">Meta AI — WA biasa (867051314767696@bot)</option><option value="718584497008509@bot">Asisten Business — WA Business (718584497008509@bot)</option></select>
<input id=msg placeholder="pesan ke @bot (contoh: buatkan gambar kucing)" style="flex:1;min-width:180px;padding:8px" onkeydown="if(event.key==='Enter')sendBot()">
<button onclick="sendBot()">Kirim</button>
</div>
<div style="font-size:11px;color:#888">Default Meta AI (WA biasa). Pilih Asisten Business untuk 718584497008509@bot.</div>
<div style="display:flex;gap:8px;align-items:center;margin:6px 0;flex-wrap:wrap">
<label style="font-size:12px"><input type=checkbox id=autoScroll checked> auto-scroll</label>
<label style="font-size:12px"><input type=checkbox id=wrapChk checked onchange="botLog.style.whiteSpace=this.checked?'pre-wrap':'pre';botLog.style.wordBreak=this.checked?'break-word':'normal'"> warp</label>
<button onclick="clearBot()">Clear</button><button onclick="copyBot()">Copy</button><span id=botSt style="font-size:12px;color:#aaa"></span>
</div>
<pre id=botLog></pre>
<div style="display:flex;gap:8px;align-items:center;margin:6px 0"><button onclick="delAllMedia()" style="background:#f44336;color:#fff;border:none;border-radius:6px">🗑 Hapus semua media</button><small id=mediaInfo style="color:#999"></small></div>
<div id=gallery style="display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:8px;margin-top:8px"></div>

<h3>log sistem</h3><div style="display:flex;gap:8px;align-items:center;margin:6px 0"><button onclick="clearLog()">Clear</button><button onclick="copyLog()">Copy</button><span id=logSt style="font-size:12px;color:#aaa"></span></div><pre id=log></pre>
<script>
function updateRateUI(j){let b=document.getElementById('rateBanner'),pb=document.querySelector('button[onclick="pair()"]');if(!b)return;if(j.rate_limited){b.style.display='block';b.textContent='⛔ '+(j.message||'WA rate limit')+' ('+j.retry_after_minutes+' menit)';if(pb)pb.disabled=true;}else{b.style.display='none';if(pb)pb.disabled=false;}}
function addMediaThumb(url){
  let g=document.getElementById('gallery');
  let el;
  if(url.endsWith('.mp4')||url.endsWith('.mov')||url.endsWith('.webm')) el=document.createElement('video');
  else el=document.createElement('img');
  el.src=url; if(el.tagName==='VIDEO'){el.controls=true;el.style.maxHeight='180px';}
  el.style.width='100%'; el.style.objectFit='cover'; el.style.border='1px solid #333'; el.style.borderRadius='6px';
  let wrap=document.createElement('div'); wrap.style.display='flex'; wrap.style.flexDirection='column'; wrap.style.gap='4px'; wrap.dataset.url=url;
  wrap.appendChild(el);
  let row=document.createElement('div'); row.style.display='flex'; row.style.gap='6px'; row.style.alignItems='center';
  let a=document.createElement('a'); a.href=url; a.textContent='⬇ '+url.split('/').pop(); a.style.fontSize='11px'; a.style.color='#4fc3f7'; a.style.flex='1'; a.download='';
  let del=document.createElement('button'); del.textContent='🗑'; del.title='hapus '+url; del.style.padding='2px 8px'; del.style.fontSize='12px';
  del.onclick=async()=>{ if(!confirm('Hapus '+url.split('/').pop()+'?'))return; try{let r=await fetch('/api/media?url='+encodeURIComponent(url),{method:'DELETE'}); if(r.ok){wrap.remove();} else alert('gagal hapus');}catch(e){alert(e.message);}};
  row.appendChild(a); row.appendChild(del);
  wrap.appendChild(row);
  g.prepend(wrap);
}
async function delAllMedia(){ if(!confirm('Hapus SEMUA media?'))return; try{let r=await fetch('/api/media',{method:'DELETE'}); let j=await r.json().catch(()=>({})); if(r.ok){document.getElementById('gallery').innerHTML=''; alert('hapus '+(j.deleted||0)+' file');} else alert('gagal');}catch(e){alert(e.message);}}
function appendBot(j){
  let t=j.t||new Date().toISOString();
  let m=j.msg||'';
  let extra=j.extra||{};
  let el=document.getElementById('botLog');
  let media = extra.media || extra.media0 || null;
  if(media){
    if(Array.isArray(media)) media.forEach(u=>addMediaThumb(u));
    else if(typeof media==='string') addMediaThumb(media);
  } else if(m.includes('/media/')){ let mm=m.match(/\/media\/[^ ,\]]+/g); if(mm) mm.forEach(u=>addMediaThumb(u));}
  if(m.includes('mmg.whatsapp.net')){let mm=m.match(/https:\/\/mmg\.whatsapp\.net[^ \"]+/g); if(mm) {} }
  el.textContent+=t+' '+m+'\n';
  if(document.getElementById('autoScroll').checked) el.scrollTop=el.scrollHeight;
}
let es=new EventSource('/events');
es.addEventListener('state',e=>{let j=JSON.parse(e.data);conn.textContent=j.logged?(j.connected?'login ✓ konek':'login ✓ putus'):(j.connected?'belum login, siap pairing':'konek…');updateRateUI(j);});
es.addEventListener('pair',e=>{let j=JSON.parse(e.data);code.textContent=j.code;});
es.addEventListener('log',e=>{
  let j=JSON.parse(e.data);
  if(j.level==='bot'){appendBot(j);return;}
  log.textContent+=j.t+' ['+j.level+'] '+j.msg+'\n';
  if(document.getElementById('autoScroll').checked) log.scrollTop=log.scrollHeight;
});
es.addEventListener('bot',e=>{
  let j=JSON.parse(e.data);
  appendBot(j);
});
es.addEventListener('botclear',()=>{botLog.textContent='';});
es.addEventListener('logclear',()=>{log.textContent='';botLog.textContent='';});
async function pair(){code.textContent='minta…';let btn=document.querySelector('button[onclick="pair()"]');if(btn)btn.disabled=true;try{let ctl=new AbortController();let tm=setTimeout(()=>ctl.abort(),20000);let r=await fetch('/api/pair',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({phone:phone.value}),signal:ctl.signal});clearTimeout(tm);let txt=await r.text().catch(()=> '');let j={};try{j=JSON.parse(txt);}catch{};if(r.ok&&j.code){code.textContent=j.code+' (60s)';}else if(r.status===429){code.textContent='⛔ '+(j.hint||j.error||txt);alert(j.hint||j.error||txt);}else{code.textContent='gagal: '+(j.error||txt||r.statusText);}}catch(e){code.textContent='gagal: '+(e.name==='AbortError'?'timeout 20s':e.message);}finally{if(btn)btn.disabled=false;}}
async function logout(){if(!confirm('Log out?'))return;let r=await fetch('/api/logout',{method:'POST'});let j=await r.json().catch(()=>({}));if(r.ok){phone.value='';code.textContent='logout ✓';}else alert(j.error||'gagal');}
async function sendBot(){let v=document.getElementById('msg').value.trim();if(!v)return;let tj=document.getElementById('botJid').value.trim()||'718584497008509@bot';let btn=document.querySelector('button[onclick="sendBot()"]');btn.disabled=true;btn.textContent='kirim…';try{let r=await fetch('/api/send',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text:v,to:tj})});let txt=await r.text().catch(()=> '');let j={};try{j=JSON.parse(txt);}catch{};if(!r.ok){alert(j.error||txt||r.statusText);}else{document.getElementById('msg').value='';}}catch(e){alert(e.message);}finally{btn.disabled=false;btn.textContent='Kirim';}}
function showMedia(v){ if(!v) return; if(Array.isArray(v)) v.forEach(u=>addMediaThumb(u)); else if(typeof v==='string') addMediaThumb(v); }
async function st0(){let r=await fetch('/api/state');let j=await r.json();phone.value=j.phone||'';if(j.code&&j.codeOk)code.textContent=j.code;conn.textContent=j.logged?(j.connected?'login ✓ konek':'login ✓ putus'):(j.connected?'belum login, siap pairing':'konek…');updateRateUI(j);let l=await(await fetch('/api/logs')).json();log.textContent='';botLog.textContent='';document.getElementById('gallery').innerHTML='';for(let e of l){try{if(e.level==='bot'){botLog.textContent+=e.t+' '+e.msg+'\n'; if(e.extra&&e.extra.media) showMedia(e.extra.media); else if(e.extra&&e.extra.media0) showMedia(e.extra.media0); else if(e.msg&&e.msg.includes('/media/')){let mm=e.msg.match(/\/media\/[^ ,\]]+/g); if(mm) mm.forEach(u=>addMediaThumb(u));}}else{log.textContent+=e.t+' ['+e.level+'] '+e.msg+'\n';}}catch(_){}}botLog.scrollTop=botLog.scrollHeight;log.scrollTop=log.scrollHeight; loadGalleryInit();}
async function clearLog(){let r=await fetch('/api/logs',{method:'DELETE'});if(r.ok){log.textContent='';botLog.textContent='';}}
async function copyLog(){let t=log.textContent;if(!t)return;try{await navigator.clipboard.writeText(t);logSt.textContent='copied';}catch(_){let ta=document.createElement('textarea');ta.value=t;document.body.appendChild(ta);ta.select();document.execCommand('copy');ta.remove();logSt.textContent='copied';}setTimeout(()=>logSt.textContent='',1500);}
async function clearBot(){let r=await fetch('/api/logs?level=bot',{method:'DELETE'});if(r.ok){botLog.textContent='';} else {botLog.textContent='';} botSt.textContent='cleared';setTimeout(()=>botSt.textContent='',1200);}
async function copyBot(){let t=botLog.textContent;if(!t)return;try{await navigator.clipboard.writeText(t);botSt.textContent='copied';}catch(_){let ta=document.createElement('textarea');ta.value=t;document.body.appendChild(ta);ta.select();document.execCommand('copy');ta.remove();botSt.textContent='copied';}setTimeout(()=>botSt.textContent='',1500);}
async function loadGalleryInit(){try{let r=await fetch('/api/media');let j=await r.json(); if(!j) j=[]; j.forEach(f=>addMediaThumb(f.url));}catch(_){}}
st0();
</script>`

func main() {
	ctx := context.Background()
	os.MkdirAll(mediaDir, 0755)
	driver, dsn := getDSN()
	container, err := sqlstore.New(ctx, driver, dsn, waLog.Stdout("db", "WARN", true))
	if err != nil {
		log.Fatal(err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		log.Fatal(err)
	}
	client := whatsmeow.NewClient(device, waLog.Stdout("wa", "INFO", true))
	b := &bridge{client: client, container: container, logs: &logStore{}}
	rawDB, _ := sql.Open(driver, dsn)
	if rawDB != nil {
		b.stateDB = rawDB
		_ = setupStateTable(b.stateDB)
		_ = setupTelemetryTable(b.stateDB)
	}
	b.mediaJobs = make(chan MediaJob, 64)
	b.stateWriteCh = make(chan stateWriteJob, 256)
	b.telemetryCh = make(chan telemetryJob, 128)
	if b.stateDB != nil {
		_ = b.LoadStateOnStartup()
		go b.stateWriter()
		go b.telemetryWriter()
		go b.startStateGC()
	}
	for i := 0; i < 4; i++ {
		go b.mediaWorker()
	}
	client.AddEventHandler(b.handle)
	b.setConn(false)
	go func() {
		last := false
		for {
			time.Sleep(3 * time.Second)
			v := b.client.IsConnected()
			if v != last {
				last = v
				b.setConn(v)
			}
		}
	}()
	if client.Store.ID == nil {
		b.phone = os.Getenv("WA_PHONE")
		if err := client.Connect(); err != nil {
			log.Fatal(err)
		}
		b.addLog("info", "siap. minta pairing code dari web (/api/pair).", nil)
	} else if err := client.Connect(); err != nil {
		log.Fatal(err)
	} else {
		b.addLog("info", "sesi pulih, terhubung. BOT="+botJID.String(), nil)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		b.pm.RLock()
		defer b.pm.RUnlock()
		rl, mins := rateStatus()
		out := map[string]any{
			"connected": b.client.IsConnected(), "logged": b.client.Store.ID != nil,
			"phone": b.phone, "code": b.pairCode, "codeOk": time.Now().Before(b.pairExp) && b.pairCode != "",
			"bot": botJID.String(),
		}
		if rl {
			out["rate_limited"] = true
			out["retry_after_minutes"] = mins
			out["message"] = fmt.Sprintf("WA rate limit, tunggu %d menit lagi", mins)
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/api/pair", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Phone string `json:"phone"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &in)
		if strings.TrimSpace(in.Phone) == "" {
			in.Phone = os.Getenv("WA_PHONE")
		}
		if strings.TrimSpace(in.Phone) == "" {
			http.Error(w, "phone kosong", 400)
			return
		}
		if rl, mins := rateStatus(); rl {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprintf("WA rate limit, tunggu %d menit lagi", mins), "rate_limited": true, "retry_after_minutes": mins, "retry_after": mins * 60, "hint": "WA rate limit pairing — tunggu 30-60 menit, jangan spam Minta code"})
			return
		}
		ctx2, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		code, err := b.requestPair(ctx2, strings.TrimSpace(in.Phone))
		if err != nil {
			msg := err.Error()
			is429 := strings.Contains(msg, "429") || strings.Contains(msg, "rate-overlimit") || strings.Contains(msg, "rate_overlimit")
			w.Header().Set("Content-Type", "application/json")
			if is429 {
				setRateLimit(45 * time.Minute)
				_, mins2 := rateStatus()
				w.WriteHeader(429)
				json.NewEncoder(w).Encode(map[string]any{"error": msg, "rate_limited": true, "retry_after_minutes": mins2, "retry_after": mins2 * 60, "hint": "WA rate limit pairing — tunggu 30-60 menit, jangan spam Minta code"})
			} else {
				w.WriteHeader(502)
				json.NewEncoder(w).Encode(map[string]any{"error": msg})
			}
			b.addLog("warn", "pair gagal "+in.Phone+": "+msg, nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"phone": in.Phone, "code": code})
	})
	mux.HandleFunc("/api/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST only", 405)
			return
		}
		if b.client.Store.ID == nil {
			http.Error(w, "belum login", 401)
			return
		}
		var in struct {
			Text string `json:"text"`
			To   string `json:"to"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &in)
		in.Text = strings.TrimSpace(in.Text)
		if in.Text == "" {
			http.Error(w, "text kosong", 400)
			return
		}
		to := botJID
		if strings.TrimSpace(in.To) != "" {
			if j, err := types.ParseJID(strings.TrimSpace(in.To)); err == nil && j.User != "" {
				to = j
			}
		}
		if !b.client.IsConnected() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]any{"error": "wa client belum connected", "hint": "reconnect dulu lalu coba lagi"})
			b.addLog("warn", "send gagal ke "+to.String()+": client belum connected", nil)
			return
		}
		ctx2, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := b.client.SendMessage(ctx2, to, &waE2E.Message{Conversation: &in.Text})
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
			b.addLog("warn", "send gagal ke "+to.String()+": "+err.Error(), nil)
			return
		}
		b.addLog("bot", fmt.Sprintf("BOT OUT %s [%s] %s", to.String(), "conversation", in.Text), map[string]any{"dir": "OUT", "text": in.Text, "to": to.String(), "chat": to.String()})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "to": to.String()})
	})
	mux.HandleFunc("/api/logout", func(w http.ResponseWriter, r *http.Request) {
		b.pm.Lock()
		b.phone = ""
		b.pairCode = ""
		b.pairExp = time.Time{}
		b.pm.Unlock()
		_ = b.client.Logout(ctx)
		b.client.Disconnect()
		time.Sleep(800 * time.Millisecond)
		if err := b.client.Connect(); err != nil {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		b.addLog("info", "logout sukses, siap pairing ulang", nil)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		ch := sse.add()
		defer sse.del(ch)
		fl, _ := w.(http.Flusher)
		fmt.Fprintf(w, "event: hello\ndata: {}\n\n")
		if fl != nil {
			fl.Flush()
		}
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case m := <-ch:
				io.WriteString(w, m)
				if fl != nil {
					fl.Flush()
				}
			case <-tick.C:
				io.WriteString(w, ": ping\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
		}
	})
	mux.HandleFunc("/api/telemetry", b.handleTelemetry)
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			if r.URL.Query().Get("level") == "bot" {
				b.logs.clearLevel("bot")
				lastBotText = sync.Map{}
				sse.send("botclear", map[string]any{"t": time.Now().Format(time.RFC3339)})
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "level": "bot"})
				return
			}
			b.logs.clear()
			sse.send("logclear", map[string]any{"t": time.Now().Format(time.RFC3339)})
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		json.NewEncoder(w).Encode(b.logs.all())
	})
	mux.HandleFunc("/api/media", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			q := r.URL.Query().Get("file")
			if q == "" {
				q = r.URL.Query().Get("url")
				if strings.HasPrefix(q, publicPrefix+"/") {
					q = strings.TrimPrefix(q, publicPrefix+"/")
				}
			}
			if q != "" {
				q = filepath.Clean(q)
				q = filepath.ToSlash(q)
				fp := filepath.Join(mediaDir, filepath.FromSlash(q))
				absMedia, _ := filepath.Abs(mediaDir)
				absFp, _ := filepath.Abs(fp)
				if !filepath.HasPrefix(absFp, absMedia) {
					w.WriteHeader(400)
					json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "path traversal blocked"})
					return
				}
				if _, err := os.Stat(fp); err != nil {
					base := filepath.Base(q)
					found := ""
					_ = filepath.WalkDir(mediaDir, func(path string, d os.DirEntry, err error) error {
						if found != "" {
							return filepath.SkipDir
						}
						if !d.IsDir() && d.Name() == base {
							found = path
						}
						return nil
					})
					if found != "" {
						fp = found
					}
				}
				if err := os.Remove(fp); err != nil {
					w.WriteHeader(404)
					json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
					return
				}
				b.addLog("info", "media dihapus: "+q, nil)
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "file": q})
				return
			}
			n := 0
			_ = filepath.WalkDir(mediaDir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				ext := strings.ToLower(filepath.Ext(d.Name()))
				if ext != ".jpeg" && ext != ".jpg" && ext != ".png" && ext != ".webp" && ext != ".mp4" && ext != ".mov" && ext != ".webm" && ext != ".bin" {
					return nil
				}
				_ = os.Remove(path)
				n++
				return nil
			})
			b.addLog("info", fmt.Sprintf("media hapus semua: %d file", n), nil)
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "deleted": n})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		allowed := map[string]bool{".jpeg": true, ".jpg": true, ".png": true, ".webp": true, ".mp4": true, ".mov": true, ".webm": true, ".bin": true}
		type item struct {
			URL string `json:"url"`
		}
		out := []item{}
		_ = filepath.WalkDir(mediaDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !allowed[ext] {
				return nil
			}
			rel, _ := filepath.Rel(mediaDir, path)
			rel = filepath.ToSlash(rel)
			out = append(out, item{URL: publicPrefix + "/" + rel})
			return nil
		})
		sort.Slice(out, func(i, j int) bool { return out[i].URL > out[j].URL })
		json.NewEncoder(w).Encode(out)
	})
	mux.Handle(publicPrefix+"/", http.StripPrefix(publicPrefix+"/", http.FileServer(http.Dir(mediaDir))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, page)
	})
	port := envStr("PORT", "8000")
	log.Printf("dengar :%s BOT=%s", port, botJID.String())
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

var _ = bytes.MinRead
