# AGENTS.md

Guidance for anyone - human or AI agent - modifying this repository. This file is
the contract: follow it unless a change explicitly and deliberately rewrites it.

## Hard rules

1. **No cgo - ever.** All platform interaction goes through `purego` (runtime
   `dlopen` / `LoadLibrary`) or existing bindings. Any GOOS/GOARCH must
   cross-build from any host with `CGO_ENABLED=0`.
2. **Root `*.go` files are prefixed `app_`, `lib_`, `bind_`, or `view_`.** No
   exceptions: a new root file must pick a prefix, and the package doc lives on
   `app_core.go`. Subpackages (`dialog/`, `notify/`, `tray/`, `demo/`) are not
   subject to this.
3. **Preserve the declarative contract.** `View` = config + post-spawn handle;
   `App` = config + runtime scope with settings frozen (snapshotted) on the first
   method call. No nested settings structs, no post-hoc mutation of `App`/`View`.
4. **Bindings are kind-dispatched, never type-reflected.** The value's kind alone
   decides the binding (function / `[2]any{getter, setter}` / constant). Never
   walk structs or maps, expand methods, or add `bind:"..."` tags.
5. **Validate binding names at window creation** - bad names fail `App.Show`
   loudly: no empty or whitespace segments, no collisions with `window.*`
   built-ins, appkit internals (`__webview__`, `__appkit*`) or the events global;
   a leaf and its namespace cannot both be bound.
6. **Respect per-platform threading.** The goroutine creating the first window is
   pinned to its OS thread; UI calls belong there. Background work re-enters via
   `View.Window(func(unsafe.Pointer))`.

## Architecture

- `app_core.go` - `App` config + lazily-created runtime scope, `Wait`/`Quit`,
  view registry.
- `app_content.go` - `App.FS` resolver (`app://` paths, MIME map, HTML templating)
  and the request/response types.
- `bind_core.go` - binding model, reflection wrapper, batch planning.
- `bind_bridge.go` - the generated JS bridge (init/bind/unbind scripts).
- `bind_events.go` - the page-side pub/sub bridge and `View.On`/`Off`/`Emit`.
- `view_core.go` - `View`/window API, `App.Show`, `State`, drag regions.
- `lib_unix.go` - the Linux engine: WebKitGTK via GTK3/GTK4.
- `demo/` - showcase app.

## Definition of done

```bash
gofmt -l .                # must print nothing
go vet ./...
go test ./...
```
