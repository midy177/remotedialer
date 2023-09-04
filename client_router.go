package remotedialer

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"io"
	"net/http"
	"sync/atomic"
)

var ClientRouter = echo.New()

type connResponseWriter struct {
	conn   io.Writer
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
