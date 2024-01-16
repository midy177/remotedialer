package remotedialer

import (
	"context"
	"fmt"
	"github.com/lxzan/gws"
	"sync"
	"time"
)

type gwsConn struct {
	sync.Mutex
	conn *gws.Conn
}

func newGWSConn(conn *gws.Conn) *gwsConn {
	return &gwsConn{
		conn: conn,
	}
}

func (w *gwsConn) WriteMessage(messageType gws.Opcode, deadline time.Time, data []byte) error {
	if deadline.IsZero() {
		w.Lock()
		defer w.Unlock()
		return w.conn.WriteMessage(messageType, data)
	}

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		w.Lock()
		defer w.Unlock()
		done <- w.conn.WriteMessage(messageType, data)
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("i/o timeout")
	case err := <-done:
		return err
	}
}
