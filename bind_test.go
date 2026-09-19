package appkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBindEntryFuncBindsDirectly(t *testing.T) {
	w := &bindMethodsWebViewStub{bound: map[string]any{}}
	sum := func(a, b int) int { return a + b }
	names, err := bindSingle(w, "sum", sum)
	if err != nil {
		t.Fatalf("bindEntry: %v", err)
	}
	if len(names) != 1 || names[0] != "sum" {
		t.Fatalf("bindEntry names = %v, want [sum]", names)
	}
	if _, ok := w.bound["sum"]; !ok {
		t.Fatal("bindEntry did not bind the function under its name")
	}
	if len(w.bound) != 1 {
		t.Fatalf("bindEntry bound %d names, want exactly 1 (no method expansion)", len(w.bound))
	}
}

func TestMakeBindingFuncAndConstant(t *testing.T) {
	sum := func(a, b int) int { return a + b }
	b, err := newBinding(sum)
	if err != nil {
		t.Fatalf("newBinding(func): %v", err)
	}
	if b.kind != bindingFunc || b.fn == nil {
		t.Fatalf("func value: kind = %v, wrapper present = %v; want bindingFunc with a wrapper", b.kind, b.fn != nil)
	}

	cases := []struct {
		name string
		v    any
		want string
	}{
		{"int", 42, "42"},
		{"string", "hi", `"hi"`},
		{"bool", true, "true"},
		{"slice", []int{1, 2}, "[1,2]"},
		{"map", map[string]int{"a": 1}, `{"a":1}`},
		{"struct", struct{ X int }{X: 3}, `{"X":3}`},
	}
	for _, c := range cases {
		b, err := newBinding(c.v)
		if err != nil {
			t.Fatalf("newBinding(%s): %v", c.name, err)
		}
		if b.kind != bindingConst || b.fn != nil {
			t.Fatalf("newBinding(%s): kind = %v, want bindingConst", c.name, b.kind)
		}
		if b.value != c.want {
			t.Fatalf("newBinding(%s): value = %s, want %s", c.name, b.value, c.want)
		}
	}
}

func TestMakeBindingCallableArity(t *testing.T) {
	cases := []struct {
		name     string
		fn       any
		settable bool
	}{
		{"getter", func() (string, error) { return "", nil }, false},
		{"setter", func(s string) error { return nil }, true},
		{"plain2", func(a, b int) int { return a + b }, false},
		{"variadic", func(xs ...int) int { return 0 }, false},
	}
	for _, c := range cases {
		b, err := newBinding(c.fn)
		if err != nil {
			t.Fatalf("newBinding(%s): %v", c.name, err)
		}
		if b.kind != bindingFunc {
			t.Fatalf("newBinding(%s): kind = %v, want bindingFunc", c.name, b.kind)
		}
		if b.settable != c.settable {
			t.Fatalf("newBinding(%s): settable = %v, want %v", c.name, b.settable, c.settable)
		}
	}
	getter := func() (string, error) { return "now", nil }
	b, err := newBinding(getter)
	if err != nil || b.settable {
		t.Fatalf("getter classification: %+v, %v", b, err)
	}
}

func TestMakeBindingRejectsNilAndUnmarshalable(t *testing.T) {
	if _, err := newBinding(nil); err == nil {
		t.Fatal("newBinding(nil) should fail")
	}
	if _, err := newBinding((func())(nil)); err == nil {
		t.Fatal("newBinding(nil func) should fail")
	}
	if _, err := newBinding(make(chan int)); err == nil {
		t.Fatal("newBinding(chan) should fail: not JSON-encodable")
	}
	if _, err := newBinding(func() {}); err != nil {
		t.Fatalf("newBinding(func) should succeed: %v", err)
	}
}

func TestMakeBindingAccessorPair(t *testing.T) {
	getSize := func() string { return "7px" }
	setSize := func(v string) {}
	for _, pair := range []any{
		[2]any{getSize, setSize},
		[]any{getSize, setSize},
		[]any{func() string { return "x" }, func(string) {}},
	} {
		b, err := newBinding(pair)
		if err != nil {
			t.Fatalf("newBinding(pair %T): %v", pair, err)
		}
		if b.kind != bindingAccessor || b.fn == nil || b.set == nil {
			t.Fatalf("pair %T: kind = %v, want bindingAccessor with get+set wrappers", pair, b.kind)
		}
	}
	if _, err := newBinding([]any{getSize}); err == nil {
		t.Fatal("one-element func slice should fail (not JSON-encodable)")
	}
	if _, err := newBinding([]any{getSize, "not a func"}); err == nil {
		t.Fatal("pair with a non-func element should fail")
	}
	if _, err := newAccessorBinding(func(v string) {}, func(string) {}); err == nil {
		t.Fatal("getter with arguments must be rejected")
	}
	if _, err := newAccessorBinding(func() string { return "" }, func(a, b string) {}); err == nil {
		t.Fatal("setter with two arguments must be rejected")
	}
	if _, err := newAccessorBinding(func() string { return "x" }, nil); err != nil {
		t.Fatalf("getter-only accessor: %v", err)
	}
	if _, err := newAccessorBinding(nil, func(v string) {}); err != nil {
		t.Fatalf("setter-only accessor: %v", err)
	}
	if _, err := newAccessorBinding(nil, nil); err == nil {
		t.Fatal("accessor with neither side must be rejected")
	}
}

func TestBindEntriesExpansion(t *testing.T) {
	entries, err := newBindings("sum", func(a, b int) int { return a + b })
	if err != nil || len(entries) != 1 || entries[0].name != "sum" || entries[0].kind != bindingFunc {
		t.Fatalf("newBindings(func) = %+v, %v; want one func entry", entries, err)
	}
	entries, err = newBindings("app.size", func() string { return "x" }, func(v string) {})
	if err != nil {
		t.Fatalf("newBindings(pair): %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("newBindings(pair) len = %d, want 3 entries", len(entries))
	}
	if entries[0].name != "app.size" || entries[0].kind != bindingAccessor {
		t.Fatalf("marker entry = %+v, want accessor at app.size", entries[0])
	}
	want := []string{accessorGetterKey("app.size"), accessorSetterKey("app.size")}
	for i, n := range []string{entries[1].name, entries[2].name} {
		if n != want[i] || entries[i+1].kind != bindingFunc {
			t.Fatalf("dispatch entry %d = %+v, want func at %s", i, entries[i+1], want[i])
		}
	}
	asValue, err := newBindings("app.size", []any{
		func() string { return "x" },
		func(v string) {},
	})
	if err != nil || len(asValue) != 3 || asValue[0].name != "app.size" {
		t.Fatalf("newBindings(pair as value) = %+v, %v", asValue, err)
	}
	if _, err := newBindings("", func() {}); err == nil {
		t.Fatal("empty binding name must be rejected")
	}
	if _, err := newBindings("x", func() {}, func() {}, func() {}); err == nil {
		t.Fatal("three values must be rejected")
	}
}

func TestValidateBindNameSegmentRules(t *testing.T) {
	bad := []string{"", ".x", "x.", "a..b", "a b", "a.b c", "a\tb"}
	for _, name := range bad {
		if err := validateBindingName(name); err == nil {
			t.Errorf("validateBindingName(%q) = nil, want error", name)
		}
	}
	good := []string{"a", "a.b.c", "demo.theme", "appkit", "x1._y"}
	for _, name := range good {
		if err := validateBindingName(name); err != nil {
			t.Errorf("validateBindingName(%q) = %v, want nil", name, err)
		}
	}
	if err := validateBindingName("a\x00b"); err == nil {
		t.Error("validateBindingName with NUL must be rejected")
	}
}

func TestBindingsReplaceRemovesOldSyntheticKeys(t *testing.T) {
	m := map[string]binding{}
	replace := func(entries []binding) { replaceBindings(m, entries) }

	fn := binding{name: "demo.x", kind: bindingFunc, settable: true}
	replace([]binding{fn})
	if len(m) != 1 {
		t.Fatalf("after function bind: %d entries, want 1: %v", len(m), m)
	}
	acc, err := newBindings("demo.x", func() string { return "v" }, func(string) error { return nil })
	if err != nil {
		t.Fatalf("newBindings: %v", err)
	}
	replace(acc)
	if len(m) != 3 {
		t.Fatalf("after accessor bind: %d entries, want 3: %v", len(m), sortedKeys(m))
	}
	replace([]binding{binding{name: "demo.x", kind: bindingFunc, gettable: true}})
	if len(m) != 1 {
		t.Fatalf("after replace accessor with function: %d entries, want 1 (no stale synthetic keys): %v", len(m), sortedKeys(m))
	}
	if _, ok := m["demo.x\x00get"]; ok {
		t.Fatal("stale accessor get key survived a replace")
	}
}

func installCallFor(t *testing.T, val any) string {
	t.Helper()
	entries, err := newBindings("demo.fn", val)
	if err != nil {
		t.Fatalf("newBindings(%T): %v", val, err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	return bindInstallExpr(entries[0])
}

func TestInstallCallAritySelection(t *testing.T) {
	if got := installCallFor(t, func() string { return "x" }); got != `onBind("demo.fn")` {
		t.Errorf("zero-arg -> %s", got)
	}
	if got := installCallFor(t, func(s string) string { return s }); got != `onBindSetter("demo.fn")` {
		t.Errorf("one-arg -> %s", got)
	}
	if got := installCallFor(t, func(a, b int) int { return a + b }); got != `onBindFn("demo.fn")` {
		t.Errorf("two-arg -> %s", got)
	}
	if got := installCallFor(t, func(args ...string) {}); got != `onBind("demo.fn")` {
		t.Errorf("empty variadic -> %s", got)
	}
	if got := installCallFor(t, func(prefix string, args ...string) {}); got != `onBindFn("demo.fn")` {
		t.Errorf("variadic with required arg -> %s", got)
	}
}

func TestCreateBindScriptSortedDeterministic(t *testing.T) {
	entries := []binding{
		{name: "zeta", kind: bindingFunc, settable: true},
		{name: "demo.a", kind: bindingConst, value: `{"k":1}`},
		{name: "alpha", kind: bindingFunc, gettable: true},
		{name: "demo.b", kind: bindingFunc},
	}
	script := buildBindScript(entries)
	zPos := strings.Index(script, `w.onBindSetter("zeta")`)
	aPos := strings.Index(script, `w.onBindValue("demo.a",`)
	alphaPos := strings.Index(script, `w.onBind("alpha")`)
	bPos := strings.Index(script, `w.onBindFn("demo.b")`)
	order := []int{alphaPos, aPos, bPos, zPos}
	for i, p := range order {
		if p < 0 {
			t.Fatalf("installer call %d missing from script:\n%s", i, script)
		}
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] > order[i] {
			t.Fatalf("script not sorted by name:\n%s", script)
		}
	}
	if !strings.HasSuffix(script, "w.freezeBinds();\n})()") {
		t.Fatalf("script must end with freezeBinds:\n%s", script)
	}
	if again := buildBindScript(entries); again != script {
		t.Fatal("buildBindScript is not deterministic for the same entries")
	}
}

func TestLiveBindScriptGuardsAndReports(t *testing.T) {
	script := buildLiveBindScript([]binding{{name: "demo.x", kind: bindingFunc, gettable: true}})
	if !strings.Contains(script, "var w=window.__webview__;if(!w){return;}") {
		t.Fatalf("live bind script lost its bridge guard: %s", script)
	}
	if !strings.Contains(script, `method:"`+methodBindError+`"`) {
		t.Fatalf("live bind script does not report install failures: %s", script)
	}
	if !strings.Contains(script, "w.freezeBinds()") {
		t.Fatalf("live bind script must freeze after installs: %s", script)
	}
	unbind := buildLiveUnbindScript("demo.x")
	if !strings.Contains(unbind, `w.onUnbind("demo.x")`) {
		t.Fatalf("live unbind script: %s", unbind)
	}
	if !strings.Contains(unbind, `method:"`+methodBindError+`"`) {
		t.Fatalf("live unbind script does not report failures: %s", unbind)
	}
	if empty := buildLiveBindScript(nil); empty != "" {
		t.Fatalf("buildLiveBindScript(nil) = %q, want empty", empty)
	}
}

func FuzzValidateBindName(f *testing.F) {
	for _, seed := range []string{"demo.theme", "a..b", ".x", "x.", "a b", "", "\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		err := validateBindingName(name)
		if err != nil {
			return
		}
		if name == "" {
			t.Fatal("accepted an empty name")
		}
		for _, seg := range strings.Split(name, ".") {
			if seg == "" {
				t.Fatalf("accepted name with an empty segment: %q", name)
			}
			if strings.ContainsAny(seg, " \t\r\n") {
				t.Fatalf("accepted name with whitespace: %q", name)
			}
		}
	})
}

func FuzzMakeFuncWrapperArgDecode(f *testing.F) {
	fn := func(prefix string, n int, rest ...float64) string { return prefix }
	wrapper, err := wrapBinding(fn)
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{`["p"]`, `["p",1]`, `["p",1,2.5]`, `["p","x"]`, `[]`, `{"a":1}`, `"p"`, `[1,2]`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = wrapper("", raw)
	})
}

func TestGeneratedScriptsParse(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	scripts := map[string]string{
		"appRegions": buildRegionScript(true, true, "unix"),
		"bridge":     buildInitScript("function(m){}"),
		"events":     buildEventsScript("events"),
		"bind": buildBindScript([]binding{
			{name: "demo.a", kind: bindingConst, value: `{"k":1}`},
			{name: "demo.b", kind: bindingAccessor},
			{name: "demo.c", kind: bindingFunc, settable: true},
			{name: "demo.d", kind: bindingFunc, gettable: true},
			{name: "demo.e", kind: bindingFunc},
		}),
		"liveBind":   buildLiveBindScript([]binding{{name: "demo.x", kind: bindingFunc, gettable: true}}),
		"liveUnbind": buildLiveUnbindScript("demo.x"),
	}
	for name, src := range scripts {
		file := filepath.Join(t.TempDir(), name+".js")
		if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		out, err := exec.Command(node, "--check", file).CombinedOutput()
		if err != nil {
			t.Fatalf("node --check failed for the %s script: %v\n%s", name, err, out)
		}
	}
}

func TestSerialQueueRunsInOrder(t *testing.T) {
	var q workQueue
	const n = 200
	got := make([]int, 0, n)
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		i, last := i, i == n-1
		q.do(func() {
			got = append(got, i)
			if last {
				close(done)
			}
		})
	}
	<-done
	if len(got) != n {
		t.Fatalf("ran %d tasks, want %d", len(got), n)
	}
	for pos, task := range got {
		if task != pos {
			t.Fatalf("task %d ran at position %d - workQueue is out of order", task, pos)
		}
	}
}
