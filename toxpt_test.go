package toxpt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opd-ai/toxcore"
	"go.opentelemetry.io/otel"
)

func mustKey(b byte) [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = b
	}
	return k
}

func createTestToxClient(t *testing.T) *toxcore.Tox {
	t.Helper()
	opts := toxcore.NewOptionsForTesting()
	opts.StartPort = 0
	opts.EndPort = 0
	tox, err := toxcore.New(opts)
	if err != nil {
		t.Fatalf("toxcore.New() error = %v", err)
	}
	return tox
}

func validConfig(t *testing.T) Config {
	return Config{
		ToxClient:      createTestToxClient(t),
		AllowedFriends: [][32]byte{mustKey(2)},
		BridgeORPort:   9001,
		Logger:         slog.Default(),
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     func(t *testing.T) Config
		wantErr bool
	}{
		{name: "valid", cfg: validConfig},
		{name: "missing client", cfg: func(t *testing.T) Config {
			c := validConfig(t)
			defer c.ToxClient.Kill()
			c.ToxClient = nil
			return c
		}, wantErr: true},
		{name: "empty allowed is ok", cfg: func(t *testing.T) Config {
			c := validConfig(t)
			c.AllowedFriends = nil
			return c
		}},
		{name: "zero bridge port", cfg: func(t *testing.T) Config {
			c := validConfig(t)
			defer c.ToxClient.Kill()
			c.BridgeORPort = 0
			return c
		}, wantErr: true},
		{name: "zero logger", cfg: func(t *testing.T) Config {
			c := validConfig(t)
			defer c.ToxClient.Kill()
			c.Logger = nil
			return c
		}, wantErr: true},
		{name: "negative inbound buffer", cfg: func(t *testing.T) Config {
			c := validConfig(t)
			defer c.ToxClient.Kill()
			c.InboundBufferSize = -1
			return c
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg(t)
			if cfg.ToxClient != nil {
				defer cfg.ToxClient.Kill()
			}
			err := cfg.Validate()
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
	if cfg.BridgeORPort == 0 {
		t.Fatal("expected non-zero OR port")
	}
	if cfg.InboundBufferSize != 16 {
		t.Fatalf("expected default inbound buffer size, got %d", cfg.InboundBufferSize)
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

type errListener struct {
	err error
}

func (l errListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }

func (l errListener) Close() error { return l.err }

func (l errListener) Addr() net.Addr { return &net.TCPAddr{} }

type scriptedListener struct {
	mu   sync.Mutex
	errs []error
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.errs) == 0 {
		return nil, net.ErrClosed
	}
	err := l.errs[0]
	l.errs = l.errs[1:]
	return nil, err
}

func (l *scriptedListener) Close() error { return nil }

func (l *scriptedListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestNewFriendACLFromSource(t *testing.T) {
	nilACL := NewFriendACLFromSource(nil)
	if nilACL == nil {
		t.Fatal("expected acl from nil source")
	}

	source := stubFriendSource{
		friends: map[uint32]*toxcore.Friend{
			1: {PublicKey: mustKey(3)},
			2: nil,
		},
	}
	acl := NewFriendACLFromSource(source)
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

func TestFramedConnReadDeadlineRespected(t *testing.T) {
	_, c2 := net.Pipe()
	defer c2.Close()

	fc := newFramedConn(c2)
	if err := fc.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}

	buf := make([]byte, 8)
	start := time.Now()
	_, err := fc.Read(buf)
	if err == nil {
		t.Fatal("expected read deadline error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("read ignored deadline; duration=%v", time.Since(start))
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestFramedConnWriteDeadlineRespected(t *testing.T) {
	c1, _ := net.Pipe()
	defer c1.Close()

	fc := newFramedConn(c1)
	if err := fc.SetWriteDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetWriteDeadline() error = %v", err)
	}

	start := time.Now()
	_, err := fc.Write([]byte("tor-cell"))
	if err == nil {
		t.Fatal("expected write deadline error")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("write ignored deadline; duration=%v", time.Since(start))
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected timeout error, got %v", err)
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
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
	cfg.ClientPublicKey = mustKey(5)
	tr, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
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
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
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
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
	tr, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer tr.Close()
	if _, err := tr.Dial(context.Background(), "tox://bridge"); err == nil {
		t.Fatal("expected error when listener is missing")
	}
}

func TestBridgeStartStop(t *testing.T) {
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
	bridge, err := NewEmbeddableBridge(cfg)
	if err != nil {
		t.Fatalf("NewEmbeddableBridge() error = %v", err)
	}
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := bridge.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestBridgeStopPropagatesCloseErrors(t *testing.T) {
	listenerErr := errors.New("listener close")
	transportErr := errors.New("transport close")
	bridge := &EmbeddableBridge{
		listener: errListener{err: listenerErr},
		transport: &ToxTransport{
			listener: &toxListener{
				closed:   make(chan struct{}),
				closeErr: transportErr,
			},
		},
	}

	err := bridge.Stop()
	if err == nil {
		t.Fatal("expected stop error")
	}
	if !errors.Is(err, listenerErr) {
		t.Fatalf("expected listener close error, got %v", err)
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("expected transport close error, got %v", err)
	}
}

func TestBridgeStartClosesOnParentContextCancellation(t *testing.T) {
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
	bridge, err := NewEmbeddableBridge(cfg)
	if err != nil {
		t.Fatalf("NewEmbeddableBridge() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		bridge.wg.Wait()
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bridge accept loop did not stop after parent context cancellation")
	}

	if err := bridge.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestAcceptLoopLogsAcceptErrors(t *testing.T) {
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
	var logs bytes.Buffer
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	bridge := &EmbeddableBridge{
		cfg:      cfg,
		listener: &scriptedListener{errs: []error{errors.New("boom"), net.ErrClosed}},
	}

	bridge.wg.Add(1)
	bridge.acceptLoop(context.Background())

	if !strings.Contains(logs.String(), "accept failed") {
		t.Fatalf("expected accept error log, got %q", logs.String())
	}
}

func TestBridgeHandleConn(t *testing.T) {
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
	orListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start fake OR listener: %v", err)
	}
	defer orListener.Close()

	cfg.BridgeORPort = uint16(orListener.Addr().(*net.TCPAddr).Port)
	bridge := &EmbeddableBridge{
		cfg:    cfg,
		tracer: otel.Tracer("test"),
	}

	orAccepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := orListener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		orAccepted <- conn
	}()

	bridgeConn, clientConn := net.Pipe()
	defer clientConn.Close()
	done := make(chan struct{})
	bridge.wg.Add(1)
	go func() {
		bridge.handleConn(context.Background(), bridgeConn)
		close(done)
	}()

	var orConn net.Conn
	select {
	case err := <-acceptErr:
		t.Fatalf("fake OR accept failed: %v", err)
	case orConn = <-orAccepted:
	}
	defer orConn.Close()

	clientToOR := []byte("client-to-or")
	if _, err := clientConn.Write(clientToOR); err != nil {
		t.Fatalf("client write failed: %v", err)
	}
	_ = orConn.SetReadDeadline(time.Now().Add(time.Second))
	gotUpstream := make([]byte, len(clientToOR))
	if _, err := io.ReadFull(orConn, gotUpstream); err != nil {
		t.Fatalf("or read failed: %v", err)
	}
	if !bytes.Equal(gotUpstream, clientToOR) {
		t.Fatalf("upstream relay mismatch: got %q want %q", gotUpstream, clientToOR)
	}

	orToClient := []byte("or-to-client")
	if _, err := orConn.Write(orToClient); err != nil {
		t.Fatalf("or write failed: %v", err)
	}
	_ = clientConn.SetReadDeadline(time.Now().Add(time.Second))
	gotDownstream := make([]byte, len(orToClient))
	if _, err := io.ReadFull(clientConn, gotDownstream); err != nil {
		t.Fatalf("client read failed: %v", err)
	}
	if !bytes.Equal(gotDownstream, orToClient) {
		t.Fatalf("downstream relay mismatch: got %q want %q", gotDownstream, orToClient)
	}

	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleConn did not return after closure")
	}
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

func TestTransportClosePropagatesListenerError(t *testing.T) {
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
	listenerErr := errors.New("listener close")
	tr := &ToxTransport{
		cfg: cfg,
		listener: &toxListener{
			closed:   make(chan struct{}),
			closeErr: listenerErr,
		},
	}

	err := tr.Close()
	if err == nil {
		t.Fatal("expected close error")
	}
	if !errors.Is(err, listenerErr) {
		t.Fatalf("expected listener close error, got %v", err)
	}
	if tr.listener != nil {
		t.Fatal("transport listener should be cleared after close")
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
	l := newToxListener(NewFriendACL([][32]byte{mustKey(1)}), slog.Default(), DefaultConfig().InboundBufferSize)
	if l.Addr() == nil {
		t.Fatal("expected listener address")
	}
}

func TestListenUsesConfiguredInboundBufferSize(t *testing.T) {
	cfg := validConfig(t)
	defer cfg.ToxClient.Kill()
	cfg.InboundBufferSize = 4
	tr, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer tr.Close()

	if _, err := tr.Listen(context.Background(), ""); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	if cap(tr.listener.inbound) != 4 {
		t.Fatalf("unexpected inbound buffer size: %d", cap(tr.listener.inbound))
	}
}
