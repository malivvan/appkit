package appkit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
)

var errInstanceRunning = errors.New("appkit: another instance is already running")

var (
	primaryMu           sync.Mutex
	primaryInstanceHeld bool
)

// instanceGuard holds the process-wide single-instance lock. Release is
// idempotent, so it can be deferred alongside other cleanup.
type instanceGuard struct {
	once    sync.Once
	relErr  error
	release func() error
}

func (l *instanceGuard) Release() error {
	l.once.Do(func() { l.relErr = l.release() })
	return l.relErr
}

func instanceFingerprint(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

func acquireGuard(id string, onMessage func([]string)) (*instanceGuard, error) {
	return acquireInstanceLock(id, onMessage)
}

func signalPeerInstance(id string, args []string) error { return sendInstanceMessage(id, args) }

// claimPrimaryInstance enforces single-instance mode when App.Exec is set: a
// first launch takes the lock and returns its release func, while a later launch
// forwards its arguments to the running instance and exits. Passing
// --new-instance skips the check entirely.
func claimPrimaryInstance(opts *appSetup) (func(), error) {
	if opts.Exec == nil {
		return func() {}, nil
	}
	id := opts.ID
	if id == "" {
		return nil, errors.New("appkit: App.ID is required when App.Exec enables single-instance mode")
	}
	if isPrimaryInstance() {
		return func() {}, nil
	}
	inst, err := acquireGuard(id, opts.Exec)
	if errors.Is(err, errInstanceRunning) {
		_ = signalPeerInstance(id, os.Args[1:])
		os.Exit(0)
	}
	if err != nil {
		return nil, err
	}
	markPrimaryInstance(true)
	return func() {
		markPrimaryInstance(false)
		_ = inst.Release()
	}, nil
}

func isPrimaryInstance() bool {
	primaryMu.Lock()
	defer primaryMu.Unlock()
	return primaryInstanceHeld
}

func markPrimaryInstance(v bool) {
	primaryMu.Lock()
	primaryInstanceHeld = v
	primaryMu.Unlock()
}
