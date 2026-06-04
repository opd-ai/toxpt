package toxpt

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/opd-ai/toxcore"
)

func mustKey(b byte) [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = b
	}
	return k
}

func validConfig() Config {
	return Config{
		ToxSecretKey:   mustKey(1),
		AllowedFriends: [][32]byte{mustKey(2)},
		ListenPort:     33445,
		BridgeORPort:   9001,
		Logger:         slog.Default(),
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "valid", cfg: validConfig()},
		{name: "missing key", cfg: func() Config { c := validConfig(); c.ToxSecretKey = [32]byte{}; return c }(), wantErr: true},
		{name: "empty allowed", cfg: func() Config { c := validConfig(); c.AllowedFriends = nil; return c }(), wantErr: true},
		{name: "zero listen", cfg: func() Config { c := validConfig(); c.ListenPort = 0; return c }(), wantErr: true},
		{name: "zero logger", cfg: func() Config { c := validConfig(); c.Logger = nil; return c }(), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.ListenPort != defaultListenPort {
		t.Fatalf("unexpected listen port: %d", cfg.ListenPort)
	}
	if cfg.BridgeORPort == 0 {
		t.Fatal("expected non-zero OR port")
	}
	if cfg.Logger == nil {
		t.Fatal("expected default logger")
	}
}

func TestFriendACLIsAuthorized(t *testing.T) {
	acl := NewFriendACL([][32]byte{mustKey(7), mustKey(9)})
	tests := []struct {
		name string
		key  [32]byte
		want bool
	}{
		{name: "match first", key: mustKey(7), want: true},
		{name: "match second", key: mustKey(9), want: true},
		{name: "no match", key: mustKey(8), want: false},
		{name: "zero key", key: [32]byte{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acl.IsAuthorized(tt.key); got != tt.want {
				t.Fatalf("IsAuthorized() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewFriendACLFromToxNil(t *testing.T) {
	acl := NewFriendACLFromTox(nil)
	if acl == nil {
		t.Fatal("expected acl")
	}
	if acl.IsAuthorized(mustKey(1)) {
		t.Fatal("nil tox should not authorize keys")
	}
}

type stubFriendSource struct {
	friends map[uint32]*toxcore.Friend
}

func (s stubFriendSource) GetFriends() map[uint32]*toxcore.Friend {
	return s.friends
}

func TestNewFriendACLFromSource(t *testing.T) {
	nilACL := newFriendACLFromSource(nil)
	if nilACL == nil {
		t.Fatal("expected acl from nil source")
	}

	source := stubFriendSource{
		friends: map[uint32]*toxcore.Friend{
			1: {PublicKey: mustKey(3)},
			2: nil,
		},
	}
	acl := newFriendACLFromSource(source)
	if !acl.IsAuthorized(mustKey(3)) {
		t.Fatal("expected stub friend key to be authorized")
	}
	if acl.IsAuthorized(mustKey(4)) {
		t.Fatal("unexpected authorization for unknown key")
	}
}

func TestNewFriendACLFromToxInstance(t *testing.T) {
	opts := toxcore.NewOptionsForTesting()
	opts.StartPort = 0
	opts.EndPort = 0
	tox, err := toxcore.New(opts)
	if err != nil {
		t.Fatalf("toxcore.New() error = %v", err)
	}
	defer tox.Kill()

	acl := NewFriendACLFromTox(tox)
	if acl == nil {
		t.Fatal("expected acl")
	}
	if acl.IsAuthorized(mustKey(3)) {
		t.Fatal("expected empty friend list to deny unknown key")
	}
}

func TestFramedConnRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	fc1 := newFramedConn(c1)
	fc2 := newFramedConn(c2)

	payload := []byte("tor-cell")
	errCh := make(chan error, 1)
	go func() {
		_, err := fc1.Write(payload)
		errCh <- err
	}()

	buf := make([]byte, 64)
	n, err := fc2.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(buf[:n]) != string(payload) {
		t.Fatalf("unexpected payload: %q", buf[:n])
	}
	if err := <-errCh; err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func TestApplyDeadlineHelpers(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := applyReadDeadlineFromContext(c1, cancelCtx); err == nil {
		t.Fatal("expected canceled read context error")
	}
	if err := applyWriteDeadlineFromContext(c1, cancelCtx); err == nil {
		t.Fatal("expected canceled write context error")
	}

	ctx, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := applyReadDeadlineFromContext(c1, ctx); err != nil {
		t.Fatalf("unexpected read deadline error: %v", err)
	}
	if err := applyWriteDeadlineFromContext(c1, ctx); err != nil {
		t.Fatalf("unexpected write deadline error: %v", err)
	}
}

func TestTransportDialUnauthorizedIsClosed(t *testing.T) {
	cfg := validConfig()
	cfg.ClientPublicKey = mustKey(5)
	tr, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	tr.newTox = func(*toxcore.Options) (*toxcore.Tox, error) { return nil, nil }
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer tr.Close()

	l, err := tr.Listen(context.Background(), "")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer l.Close()
	done := make(chan struct{})
	go func() {
		_, _ = l.Accept()
		close(done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := tr.Dial(ctx, "tox://bridge")
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 8)
	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("expected closed connection for unauthorized peer")
	}
	_ = l.Close()
	<-done
}

func TestTransportLifecycleAndInterfaceHelpers(t *testing.T) {
	cfg := validConfig()
	tr, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	if tr.Name() != "tox" {
		t.Fatalf("unexpected name: %q", tr.Name())
	}
	if len(tr.Methods()) != 1 || tr.Methods()[0] != "tox" {
		t.Fatalf("unexpected methods: %v", tr.Methods())
	}
	if tr.IsRunning() {
		t.Fatal("should not be running before Start")
	}
	if _, err := tr.Dial(context.Background(), "tox://bridge"); err == nil {
		t.Fatal("expected dial error when not running")
	}
	if _, err := tr.Listen(context.Background(), ""); err == nil {
		t.Fatal("expected listen error when not running")
	}
	tr.newTox = func(*toxcore.Options) (*toxcore.Tox, error) { return nil, nil }
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !tr.IsRunning() {
		t.Fatal("expected running after Start")
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestDialWithoutListener(t *testing.T) {
	tr, err := NewTransport(validConfig())
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	tr.newTox = func(*toxcore.Options) (*toxcore.Tox, error) { return nil, nil }
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer tr.Close()
	if _, err := tr.Dial(context.Background(), "tox://bridge"); err == nil {
		t.Fatal("expected error when listener is missing")
	}
}

func TestBridgeStartStop(t *testing.T) {
	cfg := validConfig()
	bridge, err := NewEmbeddableBridge(cfg)
	if err != nil {
		t.Fatalf("NewEmbeddableBridge() error = %v", err)
	}
	bridge.transport.newTox = func(*toxcore.Options) (*toxcore.Tox, error) { return nil, nil }
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := bridge.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestBridgeHandleConn(t *testing.T) {
	bridge, err := NewEmbeddableBridge(validConfig())
	if err != nil {
		t.Fatalf("NewEmbeddableBridge() error = %v", err)
	}
	c1, c2 := net.Pipe()
	done := make(chan struct{})
	bridge.wg.Add(1)
	go func() {
		bridge.handleConn(context.Background(), c1)
		close(done)
	}()
	_, _ = c2.Write([]byte("x"))
	_ = c2.Close()
	<-done
}

func TestWriteFramedTooLarge(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	err := writeFramed(context.Background(), c1, make([]byte, maxFrameSize+1))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("expected io.ErrShortBuffer, got %v", err)
	}
}

func TestErrorHelpers(t *testing.T) {
	base := errors.New("boom")
	if err := wrapNetwork("msg", nil); err == nil {
		t.Fatal("expected wrapped network error")
	}
	if err := wrapConfig("cfg", nil); err == nil {
		t.Fatal("expected wrapped config error")
	}
	if err := wrapProtocol("proto", nil); err == nil {
		t.Fatal("expected wrapped protocol error")
	}
	if err := wrapNetwork("msg", base); err == nil {
		t.Fatal("expected wrapped underlying network error")
	}
	if err := joinErr("prefix", nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := joinErr("prefix", base); err == nil {
		t.Fatal("expected joined error")
	}
}

func TestListenerAddr(t *testing.T) {
	l := newToxListener("", NewFriendACL([][32]byte{mustKey(1)}), slog.Default())
	if l.Addr() == nil {
		t.Fatal("expected listener address")
	}
}
