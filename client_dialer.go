package remotedialer

import (
	"context"
	"io"
	"net"
	"time"
)

type Hijacker func(conn net.Conn, proto, address string) (next bool)

var ClientHijack Hijacker = func(conn net.Conn, proto, address string) (next bool) {
	return true
}

func clientDial(ctx context.Context, dialer Dialer, conn *connection, message *message) {
	defer func(conn *connection) {
		_ = conn.Close()
	}(conn)

	// Do client hijacker
	if !ClientHijack(conn, message.proto, message.address) {
		conn.tunnelClose(io.EOF)
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
	defer func(netConn net.Conn) {
		_ = netConn.Close()
	}(netConn)

	pipe(conn, netConn)
}

func pipe(client *connection, server net.Conn) {
	ch := make(chan struct{}, 1)

	deFun := func(err error) error {
		if err == nil {
			err = io.EOF
		}
		client.doTunnelClose(err)
		_ = server.Close()
		return err
	}
	go func() {
		defer close(ch)
		_, err := io.Copy(server, client)
		_ = deFun(err)
	}()
	_, err := io.Copy(client, server)
	err = deFun(err)
	<-ch
	// Write tunnel error after no more I/O is happening, just incase messages get out of order
	client.writeErr(err)
}
