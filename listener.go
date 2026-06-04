package toxpt

import (
	"encoding/hex"
	"log/slog"
	"net"
	"sync"
	"time"
)

type inboundRequest struct {
	remoteKey [32]byte
	conn      net.Conn
}

type toxListener struct {
	addr      net.Addr
	acl       *FriendACL
	logger    *slog.Logger
	inbound   chan inboundRequest
	closed    chan struct{}
	closeErr  error
	closeOnce sync.Once
}

func newToxListener(bindAddr string, acl *FriendACL, logger *slog.Logger, bufferSize int) *toxListener {
	return &toxListener{
		addr:    &net.TCPAddr{IP: net.IPv4zero, Port: 0},
		acl:     acl,
		logger:  logger,
		inbound: make(chan inboundRequest, normalizedInboundBufferSize(bufferSize)),
		closed:  make(chan struct{}),
	}
}

func normalizedInboundBufferSize(bufferSize int) int {
	if bufferSize <= 0 {
		return DefaultConfig().InboundBufferSize
	}
	return bufferSize
}

func (l *toxListener) Accept() (net.Conn, error) {
	for {
		select {
		case <-l.closed:
			return nil, net.ErrClosed
		case req := <-l.inbound:
			if l.acl != nil && !l.acl.IsAuthorized(req.remoteKey) {
				if req.conn != nil {
					_ = req.conn.Close()
				}
				if l.logger != nil {
					l.logger.LogAttrs(nil, slog.LevelWarn, "unauthorized tox connection",
						slog.String("event", "unauthorized_connection"),
						slog.String("tox_pubkey", hex.EncodeToString(req.remoteKey[:])),
						slog.Time("timestamp", time.Now().UTC()),
					)
				}
				continue
			}
			return req.conn, nil
		}
	}
}

func (l *toxListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})
	return l.closeErr
}

func (l *toxListener) Addr() net.Addr {
	return l.addr
}
