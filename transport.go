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
	newTox   func(*toxcore.Options) (*toxcore.Tox, error)

	running atomic.Bool
	mu      sync.RWMutex
}

// NewTransport creates a new Tox pluggable transport instance.
func NewTransport(cfg Config) (*ToxTransport, error) {
	if cfg.Logger == nil {
		cfg.Logger = DefaultConfig().Logger
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = defaultListenPort
	}
	if cfg.BridgeORPort == 0 {
		cfg.BridgeORPort = DefaultConfig().BridgeORPort
	}
	if err := cfg.Validate(); err != nil {
		return nil, wrapConfig("invalid transport configuration", err)
	}

	return &ToxTransport{
		cfg:    cfg,
		acl:    NewFriendACL(cfg.AllowedFriends),
		newTox: toxcore.New,
	}, nil
}

func (t *ToxTransport) Name() string { return "tox" }

func (t *ToxTransport) Methods() []string { return []string{"tox"} }

func (t *ToxTransport) IsRunning() bool { return t.running.Load() }

// Start initializes toxcore in the strongest available mode.
func (t *ToxTransport) Start(_ context.Context) error {
	if t.running.Load() {
		return nil
	}

	options := toxcore.NewOptions()
	// Security mode: toxcore defaults to secure-by-default Noise-IK negotiation.
	// We additionally disable UDP and local discovery to force TCP-only transport,
	// reducing direct UDP metadata exposure and avoiding UDP proxy bypass behavior.
	options.UDPEnabled = false
	options.LocalDiscovery = false
	options.TCPPort = t.cfg.ListenPort
	options.StartPort = t.cfg.ListenPort
	options.EndPort = t.cfg.ListenPort
	options.SavedataType = toxcore.SaveDataTypeSecretKey
	options.SavedataData = append([]byte(nil), t.cfg.ToxSecretKey[:]...)
	options.SavedataLength = uint32(len(options.SavedataData))

	toxInstance, err := t.newTox(options)
	if err != nil {
		return wrapNetwork("failed to initialize toxcore", err)
	}

	t.mu.Lock()
	t.tox = toxInstance
	t.running.Store(true)
	t.mu.Unlock()
	return nil
}

func (t *ToxTransport) Dial(ctx context.Context, address string) (net.Conn, error) {
	if !t.running.Load() {
		return nil, wrapNetwork("transport not started", ErrNotRunning)
	}
	return t.dial(ctx, address)
}

func (t *ToxTransport) Listen(_ context.Context, bindAddr string) (net.Listener, error) {
	if !t.running.Load() {
		return nil, wrapNetwork("transport not started", ErrNotRunning)
	}

	l := newToxListener(bindAddr, t.acl, t.cfg.Logger)
	t.mu.Lock()
	t.listener = l
	t.mu.Unlock()
	return l, nil
}

func (t *ToxTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener != nil {
		_ = t.listener.Close()
		t.listener = nil
	}
	t.tox = nil
	t.running.Store(false)
	return nil
}
