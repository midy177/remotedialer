package remotedialer

import (
	"context"
	"errors"
	"fmt"
	"github.com/lxzan/gws"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

type onConnect func(context.Context, *Session) error

type Session struct {
	sync.Mutex

	nextConnID       int64
	clientKey        string
	sessionKey       int64
	conn             *gwsConn
	conns            map[int64]*connection
	remoteClientKeys map[string]map[int]bool
	auth             ConnectAuthorizer
	pingCancel       context.CancelFunc
	pingWait         chan struct{}
	dialer           Dialer
	client           bool
	onConnect        onConnect
}

// PrintTunnelData No tunnel logging by default
var PrintTunnelData bool

func init() {
	if os.Getenv("CATTLE_TUNNEL_DATA_DEBUG") == "true" {
		PrintTunnelData = true
	}
}

func NewClientSession(auth ConnectAuthorizer, conn *gws.Conn) *Session {
	return NewClientSessionWithDialer(auth, conn, nil)
}

func NewClientSessionWithoutConn(auth ConnectAuthorizer) *Session {
	return NewClientSessionWithDialerWithoutConn(auth, nil)
}

func NewClientSessionWithDialer(auth ConnectAuthorizer, conn *gws.Conn, dialer Dialer) *Session {
	return &Session{
		clientKey: "client",
		conn:      newGWSConn(conn),
		conns:     map[int64]*connection{},
		auth:      auth,
		client:    true,
		dialer:    dialer,
		pingWait:  make(chan struct{}, 1),
	}
}

func NewClientSessionWithDialerWithoutConn(auth ConnectAuthorizer, dialer Dialer) *Session {
	return &Session{
		clientKey: "client",
		conns:     map[int64]*connection{},
		auth:      auth,
		client:    true,
		dialer:    dialer,
		pingWait:  make(chan struct{}, 1),
	}
}

func newSessionWithoutConn(sessionKey int64, clientKey string) *Session {
	return &Session{
		nextConnID:       1,
		clientKey:        clientKey,
		sessionKey:       sessionKey,
		conns:            map[int64]*connection{},
		remoteClientKeys: map[string]map[int]bool{},
		pingWait:         make(chan struct{}, 1),
	}
}

func newSession(sessionKey int64, clientKey string, conn *gws.Conn) *Session {
	return &Session{
		nextConnID:       1,
		clientKey:        clientKey,
		sessionKey:       sessionKey,
		conn:             newGWSConn(conn),
		conns:            map[int64]*connection{},
		remoteClientKeys: map[string]map[int]bool{},
	}
}

func (s *Session) serveMessage(ctx context.Context, reader io.Reader) error {
	serverMessage, err := newServerMessage(reader)
	defer serverMessage.put()
	if err != nil {
		return err
	}

	if PrintTunnelData {
		logrus.Debug("REQUEST ", serverMessage)
	}

	if serverMessage.messageType == Connect {
		if s.auth == nil || !s.auth(serverMessage.proto, serverMessage.address) {
			return errors.New("connect not allowed")
		}
		s.clientConnect(ctx, serverMessage)
		return nil
	}

	s.Lock()
	if serverMessage.messageType == AddClient && s.remoteClientKeys != nil {
		err := s.addRemoteClient(serverMessage.address)
		s.Unlock()
		return err
	} else if serverMessage.messageType == RemoveClient {
		err := s.removeRemoteClient(serverMessage.address)
		s.Unlock()
		return err
	}
	conn := s.conns[serverMessage.connID]
	s.Unlock()

	if conn == nil {
		if serverMessage.messageType == Data {
			err := fmt.Errorf("connection not found %s/%d/%d", s.clientKey, s.sessionKey, serverMessage.connID)
			msg := newErrorMessage(serverMessage.connID, err)
			defer msg.put()
			_, _ = msg.WriteTo(defaultDeadline(), s.conn)
		}
		return nil
	}

	switch serverMessage.messageType {
	case Data:
		if err := conn.OnData(serverMessage); err != nil {
			s.closeConnection(serverMessage.connID, err)
		}
	case Pause:
		conn.OnPause()
	case Resume:
		conn.OnResume()
	case Error:
		s.closeConnection(serverMessage.connID, serverMessage.Err())
	default:
		logrus.Errorf("Connection (%d) sent an unknown message type (%d)", serverMessage.connID, serverMessage.messageType)
	}

	return nil
}

func defaultDeadline() time.Time {
	return time.Now().Add(time.Minute)
}

func parseAddress(address string) (string, int, error) {
	parts := strings.SplitN(address, "/", 2)
	if len(parts) != 2 {
		return "", 0, errors.New("not / separated")
	}
	v, err := strconv.Atoi(parts[1])
	return parts[0], v, err
}

func (s *Session) addRemoteClient(address string) error {
	clientKey, sessionKey, err := parseAddress(address)
	if err != nil {
		return fmt.Errorf("invalid remote Session %s: %v", address, err)
	}

	keys := s.remoteClientKeys[clientKey]
	if keys == nil {
		keys = map[int]bool{}
		s.remoteClientKeys[clientKey] = keys
	}
	keys[sessionKey] = true

	if PrintTunnelData {
		logrus.Debugf("ADD REMOTE CLIENT %s, SESSION %d", address, s.sessionKey)
	}

	return nil
}

func (s *Session) removeRemoteClient(address string) error {
	clientKey, sessionKey, err := parseAddress(address)
	if err != nil {
		return fmt.Errorf("invalid remote Session %s: %v", address, err)
	}

	keys := s.remoteClientKeys[clientKey]
	delete(keys, sessionKey)
	if len(keys) == 0 {
		delete(s.remoteClientKeys, clientKey)
	}

	if PrintTunnelData {
		logrus.Debugf("REMOVE REMOTE CLIENT %s, SESSION %d", address, s.sessionKey)
	}

	return nil
}

func (s *Session) closeConnection(connID int64, err error) {
	s.Lock()
	conn := s.conns[connID]
	delete(s.conns, connID)
	if PrintTunnelData {
		logrus.Debugf("CONNECTIONS %d %d", s.sessionKey, len(s.conns))
	}
	s.Unlock()

	if conn != nil {
		conn.tunnelClose(err)
	}
}

func (s *Session) clientConnect(ctx context.Context, message *message) {
	conn := newConnection(message.connID, s, message.proto, message.address)

	s.Lock()
	s.conns[message.connID] = conn
	if PrintTunnelData {
		logrus.Debugf("CONNECTIONS %d %d", s.sessionKey, len(s.conns))
	}
	s.Unlock()

	go clientDial(ctx, s.dialer, conn, message.proto, message.address)
}

type connResult struct {
	conn net.Conn
	err  error
}

func (s *Session) Dial(ctx context.Context, proto, address string) (net.Conn, error) {
	return s.serverConnectContext(ctx, proto, address)
}

func (s *Session) serverConnectContext(ctx context.Context, proto, address string) (net.Conn, error) {
	deadline, ok := ctx.Deadline()
	if ok {
		return s.serverConnect(deadline, proto, address)
	}

	result := make(chan connResult, 1)
	go func() {
		c, err := s.serverConnect(defaultDeadline(), proto, address)
		result <- connResult{conn: c, err: err}
	}()

	select {
	case <-ctx.Done():
		// We don't want to orphan an open connection, so we wait for the result and immediately close it
		go func() {
			r := <-result
			if r.err == nil {
				_ = r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-result:
		return r.conn, r.err
	}
}

func (s *Session) serverConnect(deadline time.Time, proto, address string) (net.Conn, error) {
	connID := atomic.AddInt64(&s.nextConnID, 1)
	conn := newConnection(connID, s, proto, address)

	s.Lock()
	s.conns[connID] = conn
	if PrintTunnelData {
		logrus.Debugf("CONNECTIONS %d %d", s.sessionKey, len(s.conns))
	}
	s.Unlock()
	msg := newConnectMessage(connID, proto, address)
	defer msg.put()
	_, err := s.writeMessage(deadline, msg)
	if err != nil {
		s.closeConnection(connID, err)
		return nil, err
	}

	return conn, err
}

func (s *Session) writeMessage(deadline time.Time, message *message) (int, error) {
	if PrintTunnelData {
		logrus.Debug("WRITE ", message)
	}
	return message.WriteTo(deadline, s.conn)
}

func (s *Session) Close() {
	s.Lock()
	defer s.Unlock()

	for _, conn := range s.conns {
		conn.tunnelClose(errors.New("tunnel disconnect"))
	}

	s.conns = map[int64]*connection{}
}

func (s *Session) sessionAdded(clientKey string, sessionKey int64) {
	client := fmt.Sprintf("%s/%d", clientKey, sessionKey)
	msg := newAddClient(client)
	defer msg.put()
	_, err := s.writeMessage(time.Time{}, msg)
	if err != nil {
		s.conn.conn.WriteClose(1000, nil)
	}
}

func (s *Session) sessionRemoved(clientKey string, sessionKey int64) {
	client := fmt.Sprintf("%s/%d", clientKey, sessionKey)
	msg := newRemoveClient(client)
	defer msg.put()
	_, err := s.writeMessage(time.Time{}, msg)
	if err != nil {
		s.conn.conn.WriteClose(1000, nil)
	}
}

// Implement gws handler

func (s *Session) OnOpen(socket *gws.Conn) {
	_ = socket.SetDeadline(time.Now().Add(PingWaitDuration))
	s.conn = newGWSConn(socket)
	if s.client {
		go func(conn *gws.Conn, ses *Session) {
			// The client sends the first ping, which is used to maintain the connection.
			_ = conn.WritePing(nil)
			t := time.NewTicker(PingWriteInterval)
			defer t.Stop()
		outer:
			for {
				select {
				case <-s.pingWait:
					logrus.Infof("clientKey: %s tunnel is close.", ses.clientKey)
					break outer
				case <-t.C:
					_ = conn.WritePing(nil)
				}
			}
		}(socket, s)
	}
}

func (s *Session) OnClose(socket *gws.Conn, err error) {
	close(s.pingWait)
}

func (s *Session) OnPing(socket *gws.Conn, payload []byte) {
	_ = socket.SetDeadline(time.Now().Add(PingWaitDuration))
	_ = socket.WritePong(nil)
	logrus.Debugf("receive a ping message!")
}

func (s *Session) OnPong(socket *gws.Conn, payload []byte) {
	_ = socket.SetDeadline(time.Now().Add(PingWaitDuration))
	logrus.Debugf("receive a pong message!")
}

func (s *Session) OnMessage(socket *gws.Conn, msg *gws.Message) {
	defer func() {
		_ = msg.Close()
	}()

	if msg.Opcode != gws.OpcodeBinary {
		socket.WriteClose(1000, nil)
		logrus.Infof("error in remotedialer server [%d]: %v", 400, errWrongMessageType)
		return
	}

	if err := s.serveMessage(context.Background(), msg.Data); err != nil {
		socket.WriteClose(1000, nil)
		logrus.Infof("error in remotedialer server [%d]: %v", 500, err)
		return
	}
}
