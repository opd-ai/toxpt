package toxpt

import (
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/opd-ai/go-tor/pkg/pt"
	"github.com/opd-ai/toxcore"
)

var _ pt.ClientTransport = (*ToxTransport)(nil)
var _ pt.ServerTransport = (*ToxTransport)(nil)

// ToxTransport implements Tor pluggable transport interfaces over Tox.
type ToxTransport struct {
	cfg      Config
	acl      *FriendACL
	tox      *toxcore.Tox
	listener *toxListener

	running atomic.Bool
	mu      sync.RWMutex
}

// NewTransport creates a new Tox pluggable transport instance.
// The provided config must include an existing ToxClient instance.
func NewTransport(cfg Config) (*ToxTransport, error) {
	cfg = withDefaultConfigValues(cfg)
	if cfg.Logger == nil {
		cfg.Logger = DefaultConfig().Logger
	}
	if err := cfg.Validate(); err != nil {
		return nil, wrapConfig("invalid transport configuration", err)
	}

	// Use explicit ACL if provided, otherwise allow all friends from the client
	var acl *FriendACL
	if len(cfg.AllowedFriends) > 0 {
		acl = NewFriendACL(cfg.AllowedFriends)
	} else {
		acl = NewFriendACLFromTox(cfg.ToxClient)
	}

	return &ToxTransport{
		cfg: cfg,
		acl: acl,
		tox: cfg.ToxClient,
	}, nil
}

func withDefaultConfigValues(cfg Config) Config {
	if cfg.BridgeORPort == 0 {
		cfg.BridgeORPort = DefaultConfig().BridgeORPort
	}
	if cfg.InboundBufferSize == 0 {
		cfg.InboundBufferSize = DefaultConfig().InboundBufferSize
	}
	return cfg
}

// Name returns the name of the transport protocol.
func (t *ToxTransport) Name() string { return "tox" }

// Methods returns the list of transport methods supported.
func (t *ToxTransport) Methods() []string { return []string{"tox"} }

// IsRunning reports whether the transport is currently running.
func (t *ToxTransport) IsRunning() bool { return t.running.Load() }

// Start marks the transport as running. The ToxClient must already be started.
func (t *ToxTransport) Start(_ context.Context) error {
	if t.running.Load() {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tox == nil {
		return wrapConfig("tox client is nil", ErrInvalidConfig)
	}

	// Refresh ACL from current friend list if no explicit friends specified
	if len(t.cfg.AllowedFriends) == 0 {
		t.acl = NewFriendACLFromTox(t.tox)
	}

	t.running.Store(true)
	return nil
}

// Dial establishes an outbound connection through the Tox transport.
// Note: The address parameter is ignored; the ClientPublicKey from Config determines the dial target.
func (t *ToxTransport) Dial(ctx context.Context, address string) (net.Conn, error) {
	if !t.running.Load() {
		return nil, wrapNetwork("transport not started", ErrNotRunning)
	}
	// Note: address parameter is part of the pt.ClientTransport interface but is ignored
	// since ClientPublicKey in config determines the dial target in Tox transport
	_ = address
	return t.dial(ctx)
}

// Listen creates a new listener for inbound connections through the Tox transport.
// The bindAddr parameter is ignored; the listener always listens on all Tox friends.
func (t *ToxTransport) Listen(_ context.Context, bindAddr string) (net.Listener, error) {
	if !t.running.Load() {
		return nil, wrapNetwork("transport not started", ErrNotRunning)
	}

	l := newToxListener(t.acl, t.cfg.Logger, t.cfg.InboundBufferSize)
	t.mu.Lock()
	t.listener = l
	t.mu.Unlock()
	return l, nil
}

// Close closes the transport and releases associated resources.
func (t *ToxTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var err error
	if t.listener != nil {
		err = joinErr("close listener", t.listener.Close())
		t.listener = nil
	}
	// Don't close tox - it's managed externally
	t.running.Store(false)
	return err
}
