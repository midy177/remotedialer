package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
)

type ResponseWriter struct {
	conn   net.Conn
	status int32
	header http.Header
}

func NewResponseWriter(conn net.Conn) *ResponseWriter {
	return &ResponseWriter{conn: conn}
}

func (w *ResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *ResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.conn.Write(b)
}

func (w *ResponseWriter) WriteHeader(statusCode int) {
	if atomic.LoadInt32(&w.status) != 0 {
		return
	}
	atomic.AddInt32(&w.status, int32(statusCode))

	// 构建HTTP响应头
	_, _ = fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\n", statusCode, http.StatusText(statusCode))
	_ = w.Header().Write(w.conn)
	_, _ = fmt.Fprint(w.conn, "\r\n")
}

func (w *ResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func (w *ResponseWriter) Flush() {

}
