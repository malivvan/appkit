//go:build unix

package appkit

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var (
	runtimeDirOnce sync.Once
	runtimeDirPath string
)

// runtimeDir is where the single-instance lock/socket live: XDG_RUNTIME_DIR when
// it exists and is writable, otherwise the system temp dir.
func runtimeDir() string {
	runtimeDirOnce.Do(func() {
		runtimeDirPath = os.TempDir()
		if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" && isDirWritable(d) {
			runtimeDirPath = d
		}
	})
	return runtimeDirPath
}

func isDirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".appkit-write-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func instanceFiles(id string) (lock, sock string) {
	stem := filepath.Join(runtimeDir(), "native-si-"+instanceFingerprint(id))
	return stem + ".lock", stem + ".sock"
}

// acquireInstanceLock takes the single-instance lock for id: an exclusive flock
// on a lock file (so a second process fails with errInstanceRunning) plus a Unix
// socket the running instance uses to receive forwarded arguments.
func acquireInstanceLock(id string, onMessage func([]string)) (*instanceGuard, error) {
	lockPath, sockPath := instanceFiles(id)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd())
	err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errInstanceRunning
		}
		return nil, err
	}

	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	go serveInstanceSocket(ln, onMessage)

	return &instanceGuard{release: func() error {
		_ = ln.Close()
		_ = os.Remove(sockPath)
		_ = syscall.Flock(fd, syscall.LOCK_UN)
		err := f.Close()
		_ = os.Remove(lockPath)
		return err
	}}, nil
}

func serveInstanceSocket(ln net.Listener, onMessage func([]string)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = conn.Close() }()
			data, err := io.ReadAll(conn)
			if err != nil {
				return
			}
			var args []string
			if json.Unmarshal(data, &args) == nil && onMessage != nil {
				onMessage(args)
			}
		}()
	}
}

func sendInstanceMessage(id string, args []string) error {
	_, sockPath := instanceFiles(id)
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}
