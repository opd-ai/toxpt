package toxpt

import (
	"context"
	"net"
)

func (t *ToxTransport) dial(ctx context.Context, _ string) (net.Conn, error) {
	t.mu.RLock()
	listener := t.listener
	clientKey := t.cfg.ClientPublicKey
	t.mu.RUnlock()

	if listener == nil {
		return nil, wrapNetwork("transport listener is not active", ErrNotRunning)
	}

	clientConn, serverConn := net.Pipe()
	request := inboundRequest{
		remoteKey: clientKey,
		conn:      newFramedConn(serverConn),
	}

	select {
	case <-ctx.Done():
		_ = clientConn.Close()
		_ = serverConn.Close()
		return nil, wrapNetwork("dial canceled", ctx.Err())
	case <-listener.closed:
		_ = clientConn.Close()
		_ = serverConn.Close()
		return nil, wrapNetwork("listener closed", net.ErrClosed)
	case listener.inbound <- request:
		return newFramedConn(clientConn), nil
	}
}
