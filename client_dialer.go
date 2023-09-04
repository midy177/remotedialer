package remotedialer

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

func clientDial(ctx context.Context, dialer Dialer, conn *connection, message *message) {
	defer conn.Close()
	// 添加clientRouteHandler
	if message.proto == "tcp" && message.address == "remotehandler:80" {
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			conn.tunnelClose(err)
			return
		}
		ClientRouter.ServeHTTP(&connResponseWriter{conn: conn}, req)
		return
	}

	var (
		netConn net.Conn
		err     error
	)

	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(time.Minute))
	if dialer == nil {
		d := net.Dialer{}
		netConn, err = d.DialContext(ctx, message.proto, message.address)
	} else {
		netConn, err = dialer(ctx, message.proto, message.address)
	}
	cancel()

	if err != nil {
		conn.tunnelClose(err)
		return
	}
	defer netConn.Close()

	pipe(conn, netConn)
}

func pipe(client *connection, server net.Conn) {
	wg := sync.WaitGroup{}
	wg.Add(1)

	close := func(err error) error {
		if err == nil {
			err = io.EOF
		}
		client.doTunnelClose(err)
		server.Close()
		return err
	}

	go func() {
		defer wg.Done()

		buf1 := clientDialBytePool.Get()
		defer clientDialBytePool.Put(buf1)

		_, err := CopyBuffer(server, client, buf1)
		close(err)
	}()

	buf2 := clientDialBytePool.Get()
	defer clientDialBytePool.Put(buf2)

	_, err := CopyBuffer(client, server, buf2)

	err = close(err)
	wg.Wait()

	// Write tunnel error after no more I/O is happening, just incase messages get out of order
	client.writeErr(err)
}

var clientDialBytePool = NewBytePool(32 * 1024)
