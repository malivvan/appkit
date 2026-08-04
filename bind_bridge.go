//go:build !js

package appkit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const initBridgeHead = `(function() {
  'use strict';
  function generateId() {
    var crypto = window.crypto || window.msCrypto;
    var bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.prototype.slice.call(bytes).map(function(n) {
      var s = n.toString(16);
      return ((s.length % 2) == 1 ? '0' : '') + s;
    }).join('');
  }
  var Webview = (function() {
    var _promises = {};
    // Every object the binding process creates - the namespace containers
    // for dotted names, the callable wrappers and the parsed constant
    // objects - is tracked here, and freezeBinds freezes them all once the
    // batch of a document is complete (the doc-start bind script ends with
    // freezeBinds; a live single-name bind freezes right after its own
    // install). Binding is therefore read-once per page: a bound namespace
    // is sealed the moment its batch finishes.
    var _created = [];
    function _track(o) {
      // Never track the global object itself: the batch only ever freezes
      // objects IT created (namespaces, wrappers, constant trees), so
      // globalThis/window can never end up frozen.
      if (o === window || o === globalThis) { return; }
      if (o !== null && (typeof o === 'object' || typeof o === 'function')) {
        _created.push(o);
      }
    }
    function _trackTree(o) {
      if (o === null || typeof o !== 'object') { return; }
      _track(o);
      for (var k in o) {
        if (Object.prototype.hasOwnProperty.call(o, k)) { _trackTree(o[k]); }
      }
    }
    function Webview_() {}
    Webview_.prototype.post = function(message) {
      return (`

const initBridgeTail = `)(message);
    };
    Webview_.prototype.call = function(method) {
      var id = generateId();
      var params = Array.prototype.slice.call(arguments, 1);
      var promise = new Promise(function(resolve, reject) {
        _promises[id] = { resolve: resolve, reject: reject };
      });
      this.post(JSON.stringify({
        id: id,
        method: method,
        params: params
      }));
      return promise;
    };
    Webview_.prototype.onReply = function(id, status, result) {
      var promise = _promises[id];
  // Settle-once: drop the entry so completed calls do not accumulate for
  // the life of the page, and ignore unknown or duplicate replies.
      delete _promises[id];
      if (!promise) {
        return;
      }
      if (result !== undefined) {
        try {
          result = JSON.parse(result);
        } catch (e) {
          promise.reject(new Error("Failed to parse binding result as JSON"));
          return;
        }
      }
      if (status === 0) {
        promise.resolve(result);
      } else {
        promise.reject(result);
      }
    };
    // _holderAt returns the object that owns the LEAF of a dotted name,
    // walking (and creating, and tracking) the intermediate namespace
    // objects as needed.
    function _holderAt(name) {
      var parts = name.split('.');
      var holder = window;
      for (var i = 0; i < parts.length - 1; i++) {
        var seg = parts[i];
        if (typeof holder[seg] !== 'object' || holder[seg] === null) {
          holder[seg] = {};
          _track(holder[seg]);
        }
        holder = holder[seg];
      }
      return { holder: holder, leaf: parts[parts.length - 1] };
    }
    function _settle(promise) {
  // Assignment syntax and fire-and-forget calls drop the promise a Go call
  // returns, so a failing write/call would otherwise surface as an UNHANDLED
  // promise rejection nobody sees. Attach a handler that re-throws on a
  // later macrotask: code that awaits the same promise still sees the
  // rejection (the handler is on the original chain and does not swallow
  // it), and genuine errors hit the console / window.onerror instead of
  // vanishing (RE1).
      if (promise && typeof promise.catch === 'function') {
        promise.catch(function(e) {
          setTimeout(function() { throw e; }, 0);
        });
      }
      return promise;
    }
    // _bindFunction installs the shared callable wrapper for a Go function
    // at a possibly dotted path: every dot walks one level deeper under
    // window, creating intermediate objects as needed, so a name like
    // "app.someAPI.call" is installed as window.app.someAPI.call. A
    // pre-existing window property at the LEAF (e.g. a browser global like
    // window.close) is overwritten - the caller explicitly asked to bind
    // this name, and failing loudly here would abort the whole bind batch
    // and leave the page's other bindings and scripts dead. (Intermediate
    // path segments are reserved for nesting: binding under a path whose
    // parent is itself a bound function replaces it with a namespace
    // object.)
    function _bindFunction(self, name) {
      var at = _holderAt(name);
      var fn = function() {
        var params = [name].concat(Array.prototype.slice.call(arguments));
        return _settle(Webview_.prototype.call.apply(self, params));
      };
      _track(fn);
      at.holder[at.leaf] = fn;
      return fn;
    }
    Webview_.prototype.onBind = function(name) {
  // Bind a ZERO-argument Go function - a callable GETTER: the page calls it
  // (window.name()), and because it needs no arguments it ALSO works as a
  // value: await window.name calls it with no arguments and resolves to
  // its result. The wrapper carries a .then so the read-by-value form is
  // meaningful. Only zero-argument functions get a .then: attaching one to
  // every bound function would make each of them a thenable, so an
  // accidental await window.fn or Promise.resolve(window.fn) would fire a
  // no-argument Go call that any function requiring arguments would reject
  // (E2).
      var self = this;
      var fn = _bindFunction(this, name);
      fn.then = function(onFulfilled, onRejected) {
        return Webview_.prototype.call.call(self, name).then(onFulfilled, onRejected);
      };
    };
    Webview_.prototype.onBindFn = function(name) {
  // Bind any OTHER Go function (two or more arguments, or variadic): it is
  // callable only - no .then and no assignment semantics, so awaiting the
  // name or wrapping it in Promise.resolve() cannot fire a call the function
  // would reject with an arguments mismatch.
      _bindFunction(this, name);
    };
    Webview_.prototype.onBindSetter = function(name) {
  // Bind a one-argument Go function as a CALLABLE SETTER at a possibly
  // dotted path: READING the property yields the callable itself, so
  // window.name(v) calls the Go function with v - and ASSIGNING to it
  // (window.name = v) also calls the function with the assigned value. One
  // name is therefore both a function and a writable variable. Assignment
  // drops the returned promise, so _settle rethrows genuine setter errors
  // on a later task (RE1); awaiting the call form (await window.name(v))
  // still surfaces them normally.
      var at = _holderAt(name);
      var self = this;
      var fn = function() {
        var params = [name].concat(Array.prototype.slice.call(arguments));
        return _settle(Webview_.prototype.call.apply(self, params));
      };
      _track(fn);
      Object.defineProperty(at.holder, at.leaf, {
        configurable: true,
        enumerable: true,
        get: function() { return fn; },
        set: function(value) {
          return _settle(Webview_.prototype.call.call(self, name, value));
        }
      });
    };
    Webview_.prototype.onBindAccessor = function(name, getKey, setKey) {
  // Bind a Go variable at a possibly dotted path as an accessor property:
  // READING the property (window.name / await window.name) calls the Go
  // getter over the bridge and resolves with its result, ASSIGNING to it
  // (window.name = v) calls the Go setter with the assigned value. Either
  // side may be missing - an empty key means the side is not exposed, so a
  // getter-only accessor installs get without set (assignment throws in
  // strict mode) and a setter-only one set without get (reads yield
  // undefined). The getter and setter dispatch through their own registry
  // keys, so the page sees a plain value that stays wired to the Go side.
      var at = _holderAt(name);
      var descriptor = { configurable: true, enumerable: true };
      if (getKey !== '') {
        descriptor.get = (function() {
          var params = [getKey].concat(Array.prototype.slice.call(arguments));
          return _settle(Webview_.prototype.call.apply(this, params));
        }).bind(this);
      }
      if (setKey !== '') {
        descriptor.set = (function() {
          var params = [setKey].concat(Array.prototype.slice.call(arguments));
          return _settle(Webview_.prototype.call.apply(this, params));
        }).bind(this);
      }
      Object.defineProperty(at.holder, at.leaf, descriptor);
    };
    Webview_.prototype.onBindValue = function(name, value) {
  // Bind a Go constant at a possibly dotted path (same namespace rules as
  // onBind): value is the raw JSON of the constant. The parsed value is
  // assigned as ONE thing under its name and every object inside it is
  // tracked, so the whole constant tree freezes with the batch - the page
  // sees a plain, immutable constant, never a way to mutate the Go-side
  // value.
      var at = _holderAt(name);
      var parsed = JSON.parse(value);
      at.holder[at.leaf] = parsed;
      _trackTree(parsed);
    };
    Webview_.prototype.freezeBinds = function() {
  // The binding process of this document is done: freeze every object it
  // created (namespace containers, callable wrappers, constant objects), so
  // the bound namespace is immutable from here on. Defensive: the global
  // object is never frozen even if it somehow ended up tracked.
      for (var i = 0; i < _created.length; i++) {
        if (_created[i] === window || _created[i] === globalThis) { continue; }
        if (!Object.isFrozen(_created[i])) { Object.freeze(_created[i]); }
      }
      _created = [];
    };
    Webview_.prototype.onUnbind = function(name) {
      var parts = name.split('.');
      var holder = window;
      for (var i = 0; i < parts.length - 1; i++) {
        holder = holder[parts[i]];
        if (!holder) {
          return;
        }
      }
      var leaf = parts[parts.length - 1];
      if (!holder.hasOwnProperty(leaf)) {
        throw new Error('Property "' + name + '" does not exist');
      }
      delete holder[leaf];
    };
    return Webview_;
  })();
  window.__webview__ = new Webview();
})()`

func buildInitScript(postFn string) string {
	return initBridgeHead + postFn + initBridgeTail
}

func buildBindScript(entries []binding) string {
	sorted := append([]binding(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
	var b strings.Builder
	b.WriteString(`(function() {
  'use strict';
  var w = window.__webview__;
  if (!w) { return; }
`)
	for _, e := range sorted {
		b.WriteString("\tw." + bindInstallExpr(e) + ";\n")
	}
	b.WriteString("\tw.freezeBinds();\n})()")
	return b.String()
}

func accessorKey(side func(id, req string) (any, error), name string, isGet bool) string {
	if side == nil {
		return ""
	}
	if isGet {
		return accessorGetterKey(name)
	}
	return accessorSetterKey(name)
}

func bindInstallExpr(e binding) string {
	switch e.kind {
	case bindingConst:
		return "onBindValue(" + jsonQuote(e.name) + "," + jsonQuote(e.value) + ")"
	case bindingAccessor:
		return "onBindAccessor(" + jsonQuote(e.name) + "," +
			jsonQuote(accessorKey(e.fn, e.name, true)) + "," +
			jsonQuote(accessorKey(e.set, e.name, false)) + ")"
	default:
		switch {
		case e.settable:
			return "onBindSetter(" + jsonQuote(e.name) + ")"
		case e.gettable:
			return "onBind(" + jsonQuote(e.name) + ")"
		default:
			return "onBindFn(" + jsonQuote(e.name) + ")"
		}
	}
}

func bindFailureJS(err, name string) string {
	return "w.post(JSON.stringify({method:" + jsonQuote(methodBindError) +
		",params:[{name:" + jsonQuote(name) +
		",error:(" + err + "&&" + err + ".message)?" + err + ".message:String(" + err + ")}]}));"
}

func buildLiveBindScript(entries []binding) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("(function(){var w=window.__webview__;if(!w){return;}")
	for _, e := range entries {
		b.WriteString("try{w." + bindInstallExpr(e) + ";}catch(err){")
		b.WriteString(bindFailureJS("err", e.name))
		b.WriteString("}")
	}
	b.WriteString("w.freezeBinds();})()")
	return b.String()
}

func buildLiveUnbindScript(name string) string {
	return "(function(){var w=window.__webview__;if(!w){return;}try{w.onUnbind(" +
		jsonQuote(name) + ");}catch(err){" + bindFailureJS("err", name) + "}})()"
}

func callBinding(fn func(id, req string) (any, error), id, req string) (status int, result string) {
	defer func() {
		r := recover()
		if r != nil {
			status = -1
			result = jsonQuote(fmt.Sprintf("binding panicked: %v", r))
		}
	}()

	resultValue, err := fn(id, req)
	if err != nil {
		return -1, jsonQuote(err.Error())
	}

	data, e := json.Marshal(resultValue)
	if e != nil {
		return -1, jsonQuote(e.Error())
	}
	return 0, string(data)
}

func jsonQuote(msg string) string {
	data, _ := json.Marshal(msg)
	return string(data)
}
