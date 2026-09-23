package appkit

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"path"
	"strings"
)

// schemeName is the custom URL scheme App.FS content is served under. Virtual
// (non-http) origins are treated as secure contexts, so the page gets
// crypto.subtle, getUserMedia and SharedArrayBuffer without a TLS certificate.
const schemeName = "app"

// Cross-origin isolation headers sent with every app:// response. They keep the
// origin "cross-origin isolated", which SharedArrayBuffer requires.
const (
	headerCOOP = "Cross-Origin-Opener-Policy"
	headerCOEP = "Cross-Origin-Embedder-Policy"
	headerCORP = "Cross-Origin-Resource-Policy"

	valSameOrigin  = "same-origin"
	valRequireCorp = "require-corp"
)

type contentRequest struct {
	Method string
	URL    string
	View   *View
}

type contentResponse struct {
	Body []byte
	MIME string
}

type contentFunc func(*contentRequest) *contentResponse

func fsContentFunc(root fs.FS, view *View) contentFunc {
	if root == nil {
		return nil
	}
	return func(r *contentRequest) *contentResponse {
		u, err := url.Parse(r.URL)
		if err != nil {
			return nil
		}
		name := strings.TrimPrefix(u.Path, "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(root, name)
		if err != nil {
			return nil
		}
		if r.View == nil {
			r.View = view
		}
		if isHTMLFile(name) {
			if rendered, ok := renderTemplate(view, name, data); ok {
				data = rendered
			}
		}
		return &contentResponse{Body: data, MIME: contentTypeByName(name)}
	}
}

func isHTMLFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

func contentTypeByName(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".wasm":
		return "application/wasm"
	default:
		return "application/octet-stream"
	}
}

func resolveAppURL(base, raw string) string {
	if base == "" || !strings.HasPrefix(raw, schemeName+"://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	out := base + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out
}

// renderTemplate executes an HTML page from App.FS as a template with the
// requesting View as its data. It uses html/template (never text/template) so
// contextual auto-escaping applies to every action: a page can never emit
// markup out of View data that was not meant as markup. A parse or execute
// failure reports ok=false, and the caller serves the original bytes verbatim.
func renderTemplate(view *View, name string, data []byte) (rendered []byte, ok bool) {
	if view == nil {
		return nil, false
	}
	tmpl, err := template.New(name).Parse(string(data))
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

func invokeContentFunc(serve contentFunc, req *contentRequest) (resp *contentResponse) {
	defer func() {
		if recover() != nil {
			resp = nil
		}
	}()
	return serve(req)
}

func contentMIME(r *contentResponse) string {
	if r.MIME != "" {
		return r.MIME
	}
	return "application/octet-stream"
}

func contentRootFor(v *View, forceLoopback bool) (base string, transient *localServer, err error) {
	s := activeRuntime.Load()
	if s == nil || s.cfg.FS == nil {
		return "", nil, nil
	}
	if !s.cfg.HTTP && !forceLoopback {
		return "", nil, nil
	}
	srv, err := s.startContentServer(v)
	if err != nil {
		return "", nil, err
	}
	if srv == nil {
		return "", nil, nil
	}
	return srv.base, srv, nil
}

func (s *appRuntime) startContentServer(v *View) (*localServer, error) {
	if s.cfg.FS == nil {
		return nil, nil
	}
	srv, _, err := startLocalServer(fsContentFunc(s.cfg.FS, v))
	if err != nil {
		return nil, fmt.Errorf("appkit: serve App.FS for a view: %w", err)
	}
	return srv, nil
}

func closeLoopbackServer(srv *localServer) {
	if srv == nil {
		return
	}
	_ = srv.Close()
}
