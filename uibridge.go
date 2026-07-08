package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// uiBridge lets the REST server drive the live Vue frontend. A command is sent
// to the webview as a Wails event; the frontend performs it (clicking a real
// button, pressing a key, typing) and calls back UIAck with the resulting
// on-screen state, which we return to the HTTP caller. This is what lets an
// agent actually operate the UI, not just the engine.
type uiBridge struct {
	app     *App
	mu      sync.Mutex
	seq     int
	pending map[string]chan json.RawMessage
	// dispatchMu serializes commands end-to-end: only one command is emitted and
	// awaited at a time, so two DOM mutations never overlap in the webview.
	dispatchMu sync.Mutex
}

type uiCommand struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // "state" | "press" | "dblclick" | "key" | "input"
	Testid string `json:"testid,omitempty"`
	Key    string `json:"key,omitempty"`
	Value  string `json:"value,omitempty"`
}

func newUIBridge(app *App) *uiBridge {
	return &uiBridge{app: app, pending: make(map[string]chan json.RawMessage)}
}

func (b *uiBridge) ack(id string, state json.RawMessage) {
	b.mu.Lock()
	ch := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if ch != nil {
		ch <- state
	}
}

func (b *uiBridge) dispatch(cmd uiCommand) (any, error) {
	if b.app.ctx == nil {
		return nil, fmt.Errorf("UI not ready")
	}

	// One command at a time. The frontend also queues, but serializing here
	// keeps each command's timeout from starting while another is still settling.
	b.dispatchMu.Lock()
	defer b.dispatchMu.Unlock()

	b.mu.Lock()
	b.seq++
	cmd.ID = fmt.Sprintf("c%d", b.seq)
	ch := make(chan json.RawMessage, 1)
	b.pending[cmd.ID] = ch
	b.mu.Unlock()

	runtime.EventsEmit(b.app.ctx, "ui:command", cmd)

	select {
	case state := <-ch:
		return state, nil
	case <-time.After(5 * time.Second):
		b.mu.Lock()
		delete(b.pending, cmd.ID)
		b.mu.Unlock()
		return nil, fmt.Errorf("UI did not respond")
	}
}

// UIController implementation ------------------------------------------------

func (b *uiBridge) State() (any, error) {
	return b.dispatch(uiCommand{Type: "state"})
}

func (b *uiBridge) Press(testid string) (any, error) {
	return b.dispatch(uiCommand{Type: "press", Testid: testid})
}

func (b *uiBridge) DblClick(testid string) (any, error) {
	return b.dispatch(uiCommand{Type: "dblclick", Testid: testid})
}

func (b *uiBridge) Key(key string) (any, error) {
	return b.dispatch(uiCommand{Type: "key", Key: key})
}

func (b *uiBridge) Input(testid, value string) (any, error) {
	return b.dispatch(uiCommand{Type: "input", Testid: testid, Value: value})
}
