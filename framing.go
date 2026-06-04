package toxpt

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/opd-ai/go-tor/pkg/pool"
)

const maxFrameSize = 64 * 1024

var frameHeaderPool = pool.NewBufferPool(maxFrameSize + 4)

func writeFramed(ctx context.Context, conn net.Conn, payload []byte) error {
	if len(payload) > maxFrameSize {
		return wrapProtocol("frame payload too large", io.ErrShortBuffer)
	}
	if err := applyWriteDeadlineFromContext(conn, ctx); err != nil {
		return err
	}

	buf := frameHeaderPool.Get()
	defer frameHeaderPool.Put(buf)

	needed := 4 + len(payload)
	if cap(buf) < needed {
		buf = make([]byte, needed)
	} else {
		buf = buf[:needed]
	}

	binary.BigEndian.PutUint32(buf[:4], uint32(len(payload)))
	copy(buf[4:], payload)

	if _, err := conn.Write(buf); err != nil {
		return wrapNetwork("failed to write framed payload", err)
	}
	return nil
}

func readFramed(ctx context.Context, conn net.Conn) ([]byte, error) {
	if err := applyReadDeadlineFromContext(conn, ctx); err != nil {
		return nil, err
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, wrapNetwork("failed to read frame header", err)
	}

	size := binary.BigEndian.Uint32(head)
	if size > maxFrameSize {
		return nil, wrapProtocol("invalid frame size", io.ErrUnexpectedEOF)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, wrapNetwork("failed to read frame payload", err)
	}
	return payload, nil
}

type framedConn struct {
	net.Conn
	mu      sync.Mutex
	readBuf []byte
}

func newFramedConn(conn net.Conn) net.Conn {
	return &framedConn{Conn: conn}
}

func (c *framedConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.readBuf) == 0 {
		payload, err := readFramed(context.Background(), c.Conn)
		if err != nil {
			return 0, err
		}
		c.readBuf = payload
	}

	n := copy(p, c.readBuf)
	c.readBuf = c.readBuf[n:]
	return n, nil
}

func (c *framedConn) Write(p []byte) (int, error) {
	if err := writeFramed(context.Background(), c.Conn, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func applyReadDeadlineFromContext(conn net.Conn, ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return wrapNetwork("failed to set read deadline", err)
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return wrapNetwork("read context canceled", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return nil
}

func applyWriteDeadlineFromContext(conn net.Conn, ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return wrapNetwork("failed to set write deadline", err)
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return wrapNetwork("write context canceled", err)
	}
	_ = conn.SetWriteDeadline(time.Time{})
	return nil
}
