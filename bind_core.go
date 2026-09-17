package appkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"
)

var errInterface = reflect.TypeFor[error]()

type bindingKind uint8

const (
	bindingFunc bindingKind = iota
	bindingConst
	bindingAccessor
)

// binding is one installed name: a Go function (kind bindingFunc, possibly a
// getter/setter accessor), an immutable constant, or an accessor pair.
type binding struct {
	name     string
	kind     bindingKind
	fn       func(id, req string) (any, error)
	set      func(id, req string) (any, error)
	settable bool
	gettable bool
	value    string
}

// newBinding classifies v by kind alone: a func becomes callable, a two-element
// [getter, setter] pair becomes an accessor, and anything else is marshalled as
// a constant. No struct walking or tags.
func newBinding(v any) (binding, error) {
	if v == nil {
		return binding{}, errors.New("cannot bind a nil value")
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Func {
		if rv.IsNil() {
			return binding{}, errors.New("cannot bind a nil function")
		}
		wrapper, err := wrapBinding(v)
		if err != nil {
			return binding{}, err
		}
		ft := rv.Type()
		required := ft.NumIn()
		if ft.IsVariadic() {
			required--
		}
		return binding{
			kind:     bindingFunc,
			fn:       wrapper,
			settable: ft.NumIn() == 1 && !ft.IsVariadic(),
			gettable: required == 0,
		}, nil
	}
	if g, s, ok := asFuncPair(v); ok {
		return newAccessorBinding(g, s)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return binding{}, err
	}
	return binding{kind: bindingConst, value: string(data)}, nil
}

func asFuncPair(v any) (getter, setter any, ok bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice {
		return nil, nil, false
	}
	if rv.Len() != 2 {
		return nil, nil, false
	}
	g, s := rv.Index(0), rv.Index(1)
	if g.Kind() == reflect.Interface {
		g = g.Elem()
	}
	if s.Kind() == reflect.Interface {
		s = s.Elem()
	}
	if !g.IsValid() || !s.IsValid() ||
		g.Kind() != reflect.Func || s.Kind() != reflect.Func {
		return nil, nil, false
	}
	if g.IsNil() || s.IsNil() {
		return nil, nil, false
	}
	return g.Interface(), s.Interface(), true
}

func newAccessorBinding(getter, setter any) (binding, error) {
	b := binding{kind: bindingAccessor}
	if getter != nil {
		g, err := wrapBinding(getter)
		if err != nil {
			return binding{}, fmt.Errorf("accessor getter: %w", err)
		}
		gv := reflect.ValueOf(getter).Type()
		if gv.NumIn() != 0 && !gv.IsVariadic() {
			return binding{}, errors.New("accessor getter must take no arguments")
		}
		b.fn = g
	}
	if setter != nil {
		s, err := wrapBinding(setter)
		if err != nil {
			return binding{}, fmt.Errorf("accessor setter: %w", err)
		}
		sv := reflect.ValueOf(setter).Type()
		if sv.NumIn() != 1 || sv.IsVariadic() {
			return binding{}, errors.New("accessor setter must take exactly one argument")
		}
		b.set = s
	}
	if b.fn == nil && b.set == nil {
		return binding{}, errors.New("accessor binding needs a getter, a setter, or both")
	}
	return b, nil
}

func accessorGetterKey(name string) string { return name + "\x00get" }

func accessorSetterKey(name string) string { return name + "\x00set" }

func isAccessorSlot(name string) bool {
	return strings.HasSuffix(name, "\x00get") || strings.HasSuffix(name, "\x00set")
}

func validateBindingName(name string) error {
	if name == "" {
		return errors.New("binding name must not be empty")
	}
	if strings.ContainsRune(name, 0) {
		return errors.New("binding name must not contain NUL")
	}
	for _, seg := range strings.Split(name, ".") {
		if seg == "" {
			return fmt.Errorf("binding name %q must not have empty dot segments", name)
		}
		if strings.IndexFunc(seg, unicode.IsSpace) >= 0 {
			return fmt.Errorf("binding name %q must not contain whitespace", name)
		}
	}
	return nil
}

func newBindings(name string, vals ...any) ([]binding, error) {
	if err := validateBindingName(name); err != nil {
		return nil, err
	}
	var entry binding
	var err error
	switch len(vals) {
	case 1:
		entry, err = newBinding(vals[0])
	case 2:
		entry, err = newAccessorBinding(vals[0], vals[1])
	default:
		return nil, fmt.Errorf("Bind expects one value or a (getter, setter) pair, got %d values", len(vals))
	}
	if err != nil {
		return nil, err
	}
	entry.name = name
	if entry.kind != bindingAccessor {
		return []binding{entry}, nil
	}
	out := []binding{entry}
	if entry.fn != nil {
		out = append(out, binding{name: accessorGetterKey(name), kind: bindingFunc, fn: entry.fn})
	}
	if entry.set != nil {
		out = append(out, binding{name: accessorSetterKey(name), kind: bindingFunc, fn: entry.set})
	}
	return out, nil
}

// wrapBinding adapts an arbitrary Go function into the bridge's uniform
// (id, req) -> (any, error) form: JSON arguments are decoded per parameter and
// the return value is normalised to none / value / error / value+error.
//
//nolint:cyclop,funlen // reflection over the caller's signature is inherently branchy; splitting it would scatter the single validation path.
func wrapBinding(f any) (func(id, req string) (any, error), error) {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func {
		return nil, errors.New("only functions can be bound")
	}

	funcType := v.Type()
	outCount := funcType.NumOut()
	if outCount > 2 {
		return nil, errors.New("function may only return a value or value+error")
	}

	numIn := funcType.NumIn()
	isVariadic := funcType.IsVariadic()
	inTypes := make([]reflect.Type, numIn)
	for i := range numIn {
		inTypes[i] = funcType.In(i)
	}

	var returnsError bool
	switch outCount {
	case 1:
		if funcType.Out(0).Implements(errInterface) {
			returnsError = true
		}
	case 2:
		if !funcType.Out(1).Implements(errInterface) {
			return nil, errors.New("second return value must implement error")
		}
	}

	fn := func(_, req string) (any, error) {
		var rawArgs []json.RawMessage
		err := json.Unmarshal([]byte(req), &rawArgs)
		if err != nil {
			return nil, err
		}
		if (!isVariadic && len(rawArgs) != numIn) || (isVariadic && len(rawArgs) < numIn-1) {
			return nil, errors.New("function arguments mismatch")
		}

		args := make([]reflect.Value, len(rawArgs))
		for i := range rawArgs {
			var argVal reflect.Value
			if isVariadic && i >= numIn-1 {
				argVal = reflect.New(inTypes[numIn-1].Elem())
			} else {
				argVal = reflect.New(inTypes[i])
			}
			err = json.Unmarshal(rawArgs[i], argVal.Interface())
			if err != nil {
				return nil, err
			}
			args[i] = argVal.Elem()
		}

		res := v.Call(args)

		switch outCount {
		case 0:
			return nil, nil //nolint:nilnil // a nil value with a nil error is the "no result" contract the bridge marshals.
		case 1:
			if returnsError {
				v := res[0].Interface()
				if v != nil {
					return nil, v.(error)
				}
				return nil, nil //nolint:nilnil // an error-only function returning nil means "no error".
			}
			return res[0].Interface(), nil
		case 2:
			var err error
			v := res[1].Interface()
			if v != nil {
				err = v.(error)
			}
			return res[0].Interface(), err
		default:
			panic("unreachable")
		}
	}

	return fn, nil
}

type stagedBind struct {
	name    string
	entries []binding
}

func stageBindings(batch []bindItem) ([]stagedBind, []binding, error) {
	prepared := make([]stagedBind, 0, len(batch))
	var live []binding
	for _, req := range batch {
		entries, err := newBindings(req.name, req.vals...)
		if err != nil {
			return nil, nil, err
		}
		prepared = append(prepared, stagedBind{name: req.name, entries: entries})
		for _, e := range entries {
			if e.kind == bindingFunc && isAccessorSlot(e.name) {
				continue
			}
			live = append(live, e)
		}
	}
	return prepared, live, nil
}

func replaceBindings(m map[string]binding, entries []binding) {
	if len(entries) == 0 {
		return
	}
	page := entries[0].name
	for _, old := range []string{page, accessorGetterKey(page), accessorSetterKey(page)} {
		delete(m, old)
	}
	for _, e := range entries {
		m[e.name] = e
	}
}

type bindFailureReport struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

func reportBindFailure(params json.RawMessage) {
	var reports []bindFailureReport
	if err := json.Unmarshal(params, &reports); err != nil || len(reports) == 0 {
		return
	}
	r := reports[0]
	log.Printf("appkit: live bind/unbind of %q failed on the page: %s", r.Name, r.Error)
}

type workQueue struct {
	mu   sync.Mutex
	prev chan struct{}
}

func (q *workQueue) do(fn func()) {
	q.mu.Lock()
	prev := q.prev
	done := make(chan struct{})
	q.prev = done
	q.mu.Unlock()
	go func() {
		if prev != nil {
			<-prev
		}
		defer close(done)
		fn()
	}()
}

type bindSink interface {
	Bind(name string, vals ...any) error
}

type bindBatchSink interface {
	BindBatch(batch []bindItem) error
}

type bindItem struct {
	name string
	vals []any
}

func bindSingle(w bindSink, name string, v any) ([]string, error) {
	if w == nil {
		return nil, fmt.Errorf("appkit: Bind requires a non-nil View")
	}
	if v == nil {
		return nil, fmt.Errorf("appkit: Bind requires a non-nil value")
	}
	if err := w.Bind(name, v); err != nil {
		return nil, fmt.Errorf("binding %s: %w", name, err)
	}
	return []string{name}, nil
}

// applyBindings installs the merged binding set on w: App.Bind first, then
// View.Bind (which overrides same-named entries, and unbinds with a nil value).
func applyBindings(w bindSink, appBinds, viewBinds map[string]any) error {
	binds, unbinds, err := planBindings(appBinds, viewBinds, eventsGlobalNameFor(w))
	if err != nil {
		return err
	}
	if len(binds) > 0 {
		if bw, ok := w.(bindBatchSink); ok {
			if err := bw.BindBatch(binds); err != nil {
				return err
			}
		} else {
			for _, r := range binds {
				if _, err := bindSingle(w, r.name, r.vals[0]); err != nil {
					return err
				}
			}
		}
	}
	for _, name := range unbinds {
		u, ok := w.(interface{ Unbind(string) error })
		if !ok {
			return fmt.Errorf("appkit: unbinding %s: engine cannot unbind", name)
		}
		if err := u.Unbind(name); err != nil {
			return fmt.Errorf("appkit: unbinding %s: %w", name, err)
		}
	}
	return nil
}

// planBindings validates every name and returns the ordered installs and
// unbinds. Order is deterministic (sorted names, App.Bind before View.Bind) so
// the page never depends on Go map iteration order.
func planBindings(appBinds, viewBinds map[string]any, eventsGlobal string) (binds []bindItem, unbinds []string, err error) {
	final := make(map[string]bool, len(appBinds)+len(viewBinds))
	check := func(name string) error {
		if err := validateBindingName(name); err != nil {
			return err
		}
		return checkBindTarget(name, eventsGlobal)
	}
	for _, name := range sortedKeys(appBinds) {
		v := appBinds[name]
		if v == nil {
			continue
		}
		if err := check(name); err != nil {
			return nil, nil, err
		}
		binds = append(binds, bindItem{name: name, vals: []any{v}})
		final[name] = true
	}
	for _, name := range sortedKeys(viewBinds) {
		v := viewBinds[name]
		if v == nil {
			if appBinds[name] == nil {
				continue
			}
			if err := validateBindingName(name); err != nil {
				return nil, nil, err
			}
			unbinds = append(unbinds, name)
			delete(final, name)
			continue
		}
		if err := check(name); err != nil {
			return nil, nil, err
		}
		binds = append(binds, bindItem{name: name, vals: []any{v}})
		final[name] = true
	}
	if err := checkNestedBindNames(final); err != nil {
		return nil, nil, err
	}
	return binds, unbinds, nil
}

func checkBindTarget(name, eventsGlobal string) error {
	top := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		top = name[:i]
	}
	if top == "__webview__" || strings.HasPrefix(top, "__appkit") {
		return fmt.Errorf("appkit: binding name %q is reserved for appkit's internal page API", name)
	}
	if top == eventsGlobal {
		return fmt.Errorf("appkit: binding name %q would replace the page's events API (window.%s)", name, eventsGlobal)
	}
	if reservedWindowNames[top] {
		return fmt.Errorf("appkit: binding name %q would replace the page's own window.%s", name, top)
	}
	return nil
}

func checkNestedBindNames(names map[string]bool) error {
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if strings.HasPrefix(keys[i], keys[i-1]+".") {
			return fmt.Errorf("appkit: binding names %q and %q collide: %q is nested under %q, and a leaf and its namespace cannot both be bound", keys[i], keys[i-1], keys[i], keys[i-1])
		}
	}
	return nil
}

var reservedWindowNames = map[string]bool{
	"close": true, "open": true, "name": true, "top": true, "parent": true,
	"self": true, "frames": true, "length": true, "status": true, "location": true,
	"history": true, "navigator": true, "document": true, "screen": true, "origin": true,
	"devicePixelRatio": true, "alert": true, "confirm": true, "prompt": true, "print": true,
	"fetch": true, "crypto": true, "localStorage": true, "sessionStorage": true, "indexedDB": true,
	"postMessage": true, "addEventListener": true, "removeEventListener": true,
	"requestAnimationFrame": true, "setTimeout": true, "setInterval": true,
	"clearTimeout": true, "clearInterval": true, "getComputedStyle": true, "matchMedia": true,
}

func eventsGlobalNameFor(w bindSink) string {
	if g, ok := w.(eventsGlobalNamer); ok {
		return g.eventsGlobalName()
	}
	return "events"
}

func cloneBinds(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Bind installs name (a dotted path) with the given value, replacing any
// previous binding of that name.
func (w *webview) Bind(name string, vals ...any) error {
	return w.BindBatch([]bindItem{{name: name, vals: vals}})
}

func (w *webview) bindingEntries() []binding {
	entries := make([]binding, 0, len(w.bindings))
	for _, e := range w.bindings {
		if e.kind == bindingFunc && isAccessorSlot(e.name) {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

func (w *webview) eventsGlobalName() string { return w.eventsGlobal }
