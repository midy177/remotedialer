package remotedialer

import (
	"bufio"
	"fmt"
	"github.com/labstack/echo/v4"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

var ClientRouter = echo.New()

type connResponseWriter struct {
	conn   io.ReadWriter
	status int32
	header http.Header
}

func (w *connResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *connResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.conn.Write(b)
}

func (w *connResponseWriter) WriteHeader(statusCode int) {
	if atomic.LoadInt32(&w.status) != 0 {
		return
	}
	atomic.AddInt32(&w.status, int32(statusCode))

	// 构建HTTP响应头
	_, _ = fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\n", statusCode, http.StatusText(statusCode))
	w.Header().Write(w.conn)
	_, _ = fmt.Fprint(w.conn, "\r\n")
}

func (w *connResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {

	return &readWriterConn{w.conn}, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

type readWriterConn struct {
	io.ReadWriter
}

func (rwc *readWriterConn) Close() error {
	if closer, ok := rwc.ReadWriter.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (rwc *readWriterConn) LocalAddr() net.Addr {
	return nil
}

func (rwc *readWriterConn) RemoteAddr() net.Addr {
	return nil
}

func (rwc *readWriterConn) SetDeadline(t time.Time) error {
	return nil
}

func (rwc *readWriterConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (rwc *readWriterConn) SetWriteDeadline(t time.Time) error {
	return nil
}
