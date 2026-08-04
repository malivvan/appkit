package appkit

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// localServerIdleTTL is how long a localServer waits with no traffic before it
// closes itself. macOS and App.HTTP serve UI over a temporary loopback origin
// that only has to outlive the first page load.
const localServerIdleTTL = 3 * time.Second

// localServer is a minimal HTTP/1.x server bound to loopback, used where an
// app:// scheme is not available (macOS WKWebView) or where App.HTTP asks for
// it. It only ever speaks to the local engine, so it implements just enough of
// the protocol: GET/HEAD, a bounded header read, and COOP/COEP/CORP so the page
// stays a cross-origin-isolated secure context.
type localServer struct {
	ln   net.Listener
	base string

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	timer  *time.Timer
}

func startLocalServer(serve contentFunc) (*localServer, string, error) {
	if serve == nil {
		return nil, "", errors.New("loopback server: nil resolver")
	}
	ln, err := listenLocalServer("")
	if err != nil {
		return nil, "", err
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, "", errors.New("loopback server: failed to read tcp listen address")
	}
	s := &localServer{
		ln:    ln,
		base:  fmt.Sprintf("http://localhost:%d", tcpAddr.Port),
		conns: make(map[net.Conn]struct{}),
	}
	s.mu.Lock()
	s.timer = time.AfterFunc(localServerIdleTTL, func() { _ = s.Close() })
	s.mu.Unlock()
	go s.serve(serve)
	return s, s.base, nil
}

// listenLocalServer opens a TCP listener that refuses anything but a loopback
// address, so the content server is never exposed beyond the machine.
func listenLocalServer(addr string) (net.Listener, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("loopback server: invalid listen address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || !ip.IsLoopback() {
		return nil, fmt.Errorf("loopback server: refusing to listen on %q; loopback only (127.0.0.1 / [::1])", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("loopback server: listen %s: %w", addr, err)
	}
	return ln, nil
}

func (s *localServer) keepAlive() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Reset(localServerIdleTTL)
	}
	s.mu.Unlock()
}

func (s *localServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *localServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	err := s.ln.Close()
	for c := range s.conns {
		_ = c.Close()
	}
	return err
}

func (s *localServer) serve(serve contentFunc) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		go func() {
			defer func() {
				_ = conn.Close()
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
			}()
			s.serveConn(conn, serve)
		}()
	}
}

func (s *localServer) serveConn(conn net.Conn, serve contentFunc) {
	s.keepAlive()
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	method, target, version, ok := splitRequestLine(line)
	if !ok || (version != "HTTP/1.0" && version != "HTTP/1.1") {
		s.writeStatus(conn, 400)
		return
	}
	var host string
	headerBytes := 0
	for {
		if headerBytes > maxRequestHeaders {
			s.writeStatus(conn, 400)
			return
		}
		header, err := br.ReadString('\n')
		if err != nil {
			return
		}
		headerBytes += len(header)
		trimmed := strings.TrimRight(header, "\r\n")
		if trimmed == "" {
			break
		}
		if name, value, ok := strings.Cut(trimmed, ":"); ok {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "host" && host == "" {
				host = strings.TrimSpace(value)
			}
		}
	}
	if method != "GET" && method != "HEAD" {
		s.writeStatus(conn, 405)
		return
	}
	if host == "" {
		host = "localhost"
	}
	reqURL := "http://" + host + target
	resp := invokeContentFunc(serve, &contentRequest{Method: method, URL: reqURL})
	if resp == nil {
		s.writeStatus(conn, 404)
		return
	}
	s.writeResponse(conn, 200, contentMIME(resp), resp.Body, method == "HEAD")
}

const maxRequestHeaders = 64 << 10

func splitRequestLine(line string) (method, target, version string, ok bool) {
	fields := strings.Fields(strings.TrimRight(line, "\r\n"))
	if len(fields) != 3 {
		return "", "", "", false
	}
	if fields[1] == "" || strings.ContainsAny(fields[1], " \t") {
		return "", "", "", false
	}
	return fields[0], fields[1], fields[2], true
}

func statusMessage(status int) string {
	switch status {
	case 200:
		return "OK"
	case 400:
		return "Bad Request"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	default:
		return "Status " + strconv.Itoa(status)
	}
}

func (s *localServer) writeResponse(conn net.Conn, status int, mime string, body []byte, head bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/1.1 %d %s\r\n", status, statusMessage(status))
	fmt.Fprintf(&b, "Content-Type: %s\r\n", mime)
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	fmt.Fprintf(&b, "%s: %s\r\n", headerCOOP, valSameOrigin)
	fmt.Fprintf(&b, "%s: %s\r\n", headerCOEP, valRequireCorp)
	fmt.Fprintf(&b, "%s: %s\r\n", headerCORP, valSameOrigin)
	b.WriteString("Connection: close\r\n\r\n")
	_, _ = conn.Write([]byte(b.String()))
	if len(body) > 0 && !head {
		_, _ = conn.Write(body)
	}
}

func (s *localServer) writeStatus(conn net.Conn, status int) {
	s.writeResponse(conn, status, "text/plain; charset=utf-8", nil, false)
}
