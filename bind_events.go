package appkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type eventsEngine interface {
	Init(js string)
	Bind(name string, vals ...any) error
	Dispatch(f func())
	Eval(js string)
}

type events struct {
	w      eventsEngine
	global string

	mu     sync.RWMutex
	subs   map[string][]subscription
	nextID uint64
}

type subscription struct {
	id      uint64
	handler func(args ...json.RawMessage)
}

const eventsBindName = "__appkit_event__"

func installEvents(w eventsEngine, global string) (*events, error) {
	if global == "" {
		global = "events"
	}
	e := &events{
		w:      w,
		global: global,
		subs:   make(map[string][]subscription),
	}
	w.Init(buildEventsScript(global))
	if err := w.Bind(eventsBindName, e.receiveFromJS); err != nil {
		return nil, fmt.Errorf("appkit: install events bridge: %w", err)
	}
	return e, nil
}

func (w *webview) installEvents() error {
	if w.events != nil {
		return nil
	}
	e, err := installEvents(w, w.eventsGlobal)
	if err != nil {
		return err
	}
	w.events = e
	return nil
}

// On subscribes handler to event name and returns a cancel func. The handler
// runs on whichever goroutine emitted (or received) the event.
func (v *View) On(name string, handler func(args ...json.RawMessage)) (cancel func()) {
	if v.w == nil {
		return func() {}
	}
	return v.w.On(name, handler)
}

// Off removes every handler subscribed to name.
func (v *View) Off(name string) {
	if v.w == nil {
		return
	}
	v.w.Off(name)
}

// Emit delivers data to listeners on both sides: Go subscribers of name, and the
// page's window.events (or whatever App.Events renames it to).
func (v *View) Emit(name string, data ...any) error {
	if v.w == nil {
		return notShown()
	}
	return v.w.Emit(name, data...)
}

func (w *webview) On(name string, handler func(args ...json.RawMessage)) (cancel func()) {
	if w.events == nil {
		return func() {}
	}
	return w.events.On(name, handler)
}

func (w *webview) Off(name string) {
	if w.events == nil {
		return
	}
	w.events.Off(name)
}

func (w *webview) Emit(name string, data ...any) error {
	if w.events == nil {
		return errors.New("appkit: events bridge is not installed on this view")
	}
	return w.events.Emit(name, data...)
}

func (e *events) On(name string, handler func(args ...json.RawMessage)) (cancel func()) {
	e.mu.Lock()
	e.nextID++
	id := e.nextID
	e.subs[name] = append(e.subs[name], subscription{id: id, handler: handler})
	e.mu.Unlock()
	return func() { e.remove(name, id) }
}

func (e *events) Off(name string) {
	e.mu.Lock()
	delete(e.subs, name)
	e.mu.Unlock()
}

func (e *events) Emit(name string, data ...any) error {
	raw := make([]json.RawMessage, len(data))
	parts := make([]string, len(data))
	for i := range data {
		b, err := json.Marshal(data[i])
		if err != nil {
			return fmt.Errorf("appkit: encode event %q argument %d: %w", name, i, err)
		}
		raw[i] = b
		parts[i] = string(b)
	}

	e.dispatch(name, raw)

	payload := "[" + strings.Join(parts, ",") + "]"
	js := "(function(){var g=window." + e.global + ";if(g&&g._dispatch){g._dispatch(" + jsonQuote(name) + "," + payload + ");}})()"
	e.w.Dispatch(func() { e.w.Eval(js) })
	return nil
}

func (e *events) receiveFromJS(name string, args []json.RawMessage) {
	e.dispatch(name, args)
}

func (e *events) dispatch(name string, args []json.RawMessage) {
	e.mu.RLock()
	subs := e.subs[name]
	handlers := make([]func(args ...json.RawMessage), len(subs))
	for i := range subs {
		handlers[i] = subs[i].handler
	}
	e.mu.RUnlock()

	for _, h := range handlers {
		h(args...)
	}
}

func (e *events) remove(name string, id uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	subs := e.subs[name]
	for i := range subs {
		if subs[i].id == id {
			e.subs[name] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(e.subs[name]) == 0 {
		delete(e.subs, name)
	}
}

// buildEventsScript returns the page-side pub/sub bridge installed under global
// (window.<global>): on/off/emit for the page, plus _dispatch used by Go's Emit.
func buildEventsScript(global string) string {
	const tpl = `(function() {
  'use strict';
  if (window.__API__ && window.__API__._dispatch) { return; }
  var listeners = {};
  function on(name, fn) {
    (listeners[name] = listeners[name] || []).push(fn);
    return function() { off(name, fn); };
  }
  function off(name, fn) {
    if (!listeners[name]) { return; }
    if (!fn) { delete listeners[name]; return; }
    listeners[name] = listeners[name].filter(function(f) { return f !== fn; });
    if (listeners[name].length === 0) { delete listeners[name]; }
  }
  function fire(name, args) {
    var fns = listeners[name];
    if (!fns) { return; }
    fns.slice().forEach(function(fn) {
      try {
        fn.apply(null, args);
      } catch (e) {
        console.error('appkit: event handler for "' + name + '" threw:', e);
      }
    });
  }
  function emit(name) {
    var args = Array.prototype.slice.call(arguments, 1);
    fire(name, args);
    if (typeof window.__appkit_event__ === 'function') {
      var promise = window.__appkit_event__(name, args);
      if (promise && typeof promise.catch === 'function') { promise.catch(function() {}); }
    }
  }
  function _dispatch(name, args) { fire(name, args); }
  window.__API__ = { on: on, off: off, emit: emit, _dispatch: _dispatch };
})()`
	return strings.ReplaceAll(tpl, "__API__", global)
}

type eventsGlobalNamer interface {
	eventsGlobalName() string
}
