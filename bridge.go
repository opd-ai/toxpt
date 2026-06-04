package toxpt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// EmbeddableBridge manages bridge lifecycle for toxpt.
type EmbeddableBridge struct {
	cfg       Config
	transport *ToxTransport
	listener  net.Listener

	tracer   trace.Tracer
	meter    metric.Meter
	accepted metric.Int64Counter

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewEmbeddableBridge creates a bridge with explicit configuration.
func NewEmbeddableBridge(cfg Config) (*EmbeddableBridge, error) {
	transport, err := NewTransport(cfg)
	if err != nil {
		return nil, err
	}

	meter := otel.Meter("github.com/opd-ai/toxpt")
	accepted, _ := meter.Int64Counter("toxpt.bridge.connections.accepted")

	return &EmbeddableBridge{
		cfg:       cfg,
		transport: transport,
		tracer:    otel.Tracer("github.com/opd-ai/toxpt"),
		meter:     meter,
		accepted:  accepted,
	}, nil
}

// Start starts the bridge lifecycle.
func (b *EmbeddableBridge) Start(ctx context.Context) error {
	ctx, span := b.tracer.Start(ctx, "toxpt.bridge.start")
	defer span.End()

	if err := b.transport.Start(ctx); err != nil {
		return joinErr("start transport", err)
	}
	listener, err := b.transport.Listen(ctx, "")
	if err != nil {
		return joinErr("start listener", err)
	}

	b.listener = listener
	acceptCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

	b.wg.Add(2)
	go b.acceptLoop(acceptCtx)
	go b.closeListenerOnContextDone(acceptCtx, listener)
	return nil
}

func (b *EmbeddableBridge) acceptLoop(ctx context.Context) {
	defer b.wg.Done()
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			if b.shouldStopAcceptLoop(ctx, err) {
				return
			}
			b.logAcceptError(err)
			continue
		}
		b.accepted.Add(ctx, 1)
		b.wg.Add(1)
		go b.handleConn(ctx, conn)
	}
}

func (b *EmbeddableBridge) closeListenerOnContextDone(ctx context.Context, listener net.Listener) {
	defer b.wg.Done()
	<-ctx.Done()
	if listener != nil {
		_ = listener.Close()
	}
}

func (b *EmbeddableBridge) shouldStopAcceptLoop(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, net.ErrClosed)
}

func (b *EmbeddableBridge) logAcceptError(err error) {
	b.cfg.Logger.Error("accept failed", "error", err)
}

func (b *EmbeddableBridge) handleConn(ctx context.Context, conn net.Conn) {
	defer b.wg.Done()
	_, span := b.tracer.Start(ctx, "toxpt.bridge.handle_conn")
	defer span.End()

	orAddr := fmt.Sprintf("127.0.0.1:%d", b.cfg.BridgeORPort)
	dialer := net.Dialer{}
	orConn, err := dialer.DialContext(ctx, "tcp", orAddr)
	if err != nil {
		b.cfg.Logger.Error("failed to connect to tor or port", "error", err)
		return
	}
	var wg sync.WaitGroup
	wg.Add(2)
	relayDone := make(chan struct{})
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = conn.Close()
			_ = orConn.Close()
		})
	}
	defer closeBoth()

	go func() {
		select {
		case <-ctx.Done():
			closeBoth()
		case <-relayDone:
		}
	}()

	// Client -> Tor
	go func() {
		defer wg.Done()
		_, _ = io.Copy(orConn, conn)
		closeBoth()
	}()

	// Tor -> Client
	go func() {
		defer wg.Done()
		_, _ = io.Copy(conn, orConn)
		closeBoth()
	}()

	wg.Wait()
	close(relayDone)
}

// Stop gracefully stops the bridge and drains in-flight connections.
func (b *EmbeddableBridge) Stop() error {
	if b.cancel != nil {
		b.cancel()
	}
	errs := []error{
		closeWithPrefix("close listener", b.listener),
		closeWithPrefix("close transport", b.transport),
	}
	b.wg.Wait()
	return errors.Join(errs...)
}

type closer interface {
	Close() error
}

func closeWithPrefix(prefix string, c closer) error {
	if c == nil {
		return nil
	}
	return joinErr(prefix, c.Close())
}
