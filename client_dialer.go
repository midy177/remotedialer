package remotedialer

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

func clientDial(ctx context.Context, dialer Dialer, conn *connection, message *message) {
	defer conn.Close()

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

		_, err := io.CopyBuffer(server, client, buf1)
		//io.Copy(server, client)
		close(err)
	}()
	buf2 := clientDialBytePool.Get()
	defer clientDialBytePool.Put(buf2)

	_, err := io.CopyBuffer(client, server, buf2)

	//_, err := io.Copy(client, server)
	err = close(err)
	wg.Wait()

	// Write tunnel error after no more I/O is happening, just incase messages get out of order
	client.writeErr(err)
}

// 初始化sync.Pool，new函数就是创建Person结构体
func initCopyBufferPool() *sync.Pool {
	return &sync.Pool{
		New: func() interface{} {
			fmt.Println("创建一个 person.")
			return make([]byte, 32*1024)
		},
	}
}

var clientDialBytePool = NewBytePool(32 * 1024)
