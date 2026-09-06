package main

import (
	"encoding/json"
	"sync"
)

type hub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func newHub() *hub { return &hub{subs: map[chan string]struct{}{}} }
func (h *hub) add() chan string {
	c := make(chan string, 64)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	return c
}
func (h *hub) del(c chan string) { h.mu.Lock(); delete(h.subs, c); h.mu.Unlock() }
func (h *hub) send(ev string, v any) {
	b, _ := json.Marshal(v)
	msg := "event: " + ev + "\ndata: " + string(b) + "\n\n"
	h.mu.Lock()
	for c := range h.subs {
		select {
		case c <- msg:
		default:
		}
	}
	h.mu.Unlock()
}

var sse = newHub()
