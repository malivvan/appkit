package appkit

import (
	"fmt"
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
		return &contentResponse{Body: data, MIME: contentTypeByName(name)}
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
	return out
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
