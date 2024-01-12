package remotedialer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// ConnectAuthorizer custom for authorization
type ConnectAuthorizer func(proto, address string) bool

// ClientConnect connect to WS and wait 5 seconds when error
func ClientConnect(ctx context.Context, wsURL string, headers http.Header, dialer *websocket.Dialer,
	auth ConnectAuthorizer, onConnect func(context.Context, *Session) error) error {
	if err := ConnectToProxy(ctx, wsURL, headers, auth, dialer, onConnect); err != nil {
		if !errors.Is(err, context.Canceled) {
			logrus.WithError(err).Error("RemoteDialer proxy error")
			time.Sleep(time.Duration(5) * time.Second)
		}
		return err
	}
	return nil
}

// ConnectToProxy connect to websocket server
func ConnectToProxy(rootCtx context.Context, proxyURL string, headers http.Header, auth ConnectAuthorizer, dialer *websocket.Dialer, onConnect func(context.Context, *Session) error) error {
	logrus.WithField("url", proxyURL).Info("Connecting to proxy")

	if dialer == nil {
		dialer = &websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: HandshakeTimeOut}
	}
	ws, resp, err := dialer.DialContext(rootCtx, proxyURL, headers)
	if err != nil {
		if resp == nil {
			if !errors.Is(err, context.Canceled) {
				logrus.WithError(err).Errorf("Failed to connect to proxy. Empty dialer response")
			}
		} else {
			rb, err2 := io.ReadAll(resp.Body)
			if err2 != nil {
				logrus.WithError(err).Errorf("Failed to connect to proxy. Response status: %v - %v. Couldn't read response body (err: %v)", resp.StatusCode, resp.Status, err2)
			} else {
				logrus.WithError(err).Errorf("Failed to connect to proxy. Response status: %v - %v. Response body: %s", resp.StatusCode, resp.Status, rb)
			}
		}
		return err
	}
	defer func(ws *websocket.Conn) {
		_ = ws.Close()
	}(ws)

	result := make(chan error, 2)

	ctx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	session := NewClientSession(auth, ws)
	defer session.Close()

	if onConnect != nil {
		go func() {
			if err := onConnect(ctx, session); err != nil {
				result <- err
			}
		}()
	}

	go func() {
		_, err = session.Serve(ctx)
		result <- err
	}()

	select {
	case <-ctx.Done():
		logrus.WithField("url", proxyURL).WithField("err", ctx.Err()).Info("Proxy done")
		return nil
	case err := <-result:
		return err
	}
}
