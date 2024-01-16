package remotedialer

import (
	"context"
	"errors"
	"github.com/lxzan/gws"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// ConnectAuthorizer custom for authorization
type ConnectAuthorizer func(proto, address string) bool

// ClientConnect connect to WS and wait 5 seconds when error
func ClientConnect(ctx context.Context, wsURL string, headers http.Header, dialer gws.Dialer,
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
func ConnectToProxy(rootCtx context.Context, proxyURL string, headers http.Header, auth ConnectAuthorizer, dialer gws.Dialer, onConnect func(context.Context, *Session) error) error {
	logrus.WithField("url", proxyURL).Info("Connecting to proxy")

	clientOption := DefaultClientOption(proxyURL, headers)
	if dialer != nil {
		clientOption.NewDialer = func() (gws.Dialer, error) {
			return dialer, nil
		}
	}

	session := NewClientSessionWithoutConn(auth)
	defer session.Close()

	app, _, err := gws.NewClient(session, clientOption)
	if err != nil {
		return err
	}
	result := make(chan error, 1)

	ctx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	go func() {
		app.ReadLoop()
		cancel()
	}()

	if onConnect != nil {
		go func() {
			time.Sleep(PingWriteInterval)
			if err := onConnect(ctx, session); err != nil {
				result <- err
			}
		}()
	}

	select {
	case <-ctx.Done():
		logrus.WithField("url", proxyURL).WithField("err", ctx.Err()).Info("Proxy done")
		return nil
	case err := <-result:
		return err
	}
}
