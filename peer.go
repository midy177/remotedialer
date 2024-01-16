package remotedialer

import (
	"context"
	"fmt"
	"github.com/lxzan/gws"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/midy177/remotedialer/metrics"
	"github.com/sirupsen/logrus"
)

var (
	Token = "X-API-Tunnel-Token"
	ID    = "X-API-Tunnel-ID"
)

func (s *Server) AddPeer(url, id, token string) {
	if s.PeerID == "" || s.PeerToken == "" {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	NewP := &peer{
		url:    url,
		id:     id,
		token:  token,
		cancel: cancel,
	}

	logrus.Infof("Adding p %s, %s", url, id)

	s.peerLock.Lock()
	defer s.peerLock.Unlock()

	if p, ok := s.peers[id]; ok {
		if p.equals(NewP) {
			return
		}
		p.cancel()
	}

	s.peers[id] = NewP
	go NewP.start(ctx, s)
}

func (s *Server) RemovePeer(id string) {
	s.peerLock.Lock()
	defer s.peerLock.Unlock()

	if p, ok := s.peers[id]; ok {
		logrus.Infof("Removing peer %s", id)
		p.cancel()
	}
	delete(s.peers, id)
}

type peer struct {
	url, id, token string
	cancel         func()
}

func (p *peer) equals(other *peer) bool {
	return p.url == other.url &&
		p.id == other.id &&
		p.token == other.token
}

func (p *peer) start(ctx context.Context, s *Server) {
	headers := http.Header{
		ID:    {s.PeerID},
		Token: {s.PeerToken},
	}

	//dialer := &gws.Dialer{
	//	TLSClientConfig: &tls.Config{
	//		InsecureSkipVerify: true,
	//	},
	//	HandshakeTimeout: HandshakeTimeOut,
	//}

outer:
	for {
		select {
		case <-ctx.Done():
			break outer
		default:
		}

		session := NewClientSessionWithoutConn(func(string, string) bool { return true })
		session.dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			parts := strings.SplitN(network, "::", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid clientKey/proto: %s", network)
			}
			d := s.Dialer(parts[0])
			return d(ctx, parts[1], address)
		}
		clientOption := DefaultClientOption(p.url, headers)
		metrics.IncSMTotalAddPeerAttempt(p.id)
		app, _, err := gws.NewClient(session, clientOption)
		if err != nil {
			logrus.Errorf("Failed to connect to peer %s [local ID=%s]: %v", p.url, s.PeerID, err)
			time.Sleep(5 * time.Second)
			continue
		}
		// The client sends the first ping, which is used to maintain the connection.
		_ = app.WritePing(nil)
		metrics.IncSMTotalPeerConnected(p.id)

		s.sessions.addListener(session)
		//_, err = session.Serve(ctx)
		app.ReadLoop()
		s.sessions.removeListener(session)
		session.Close()
		if err != nil {
			logrus.Errorf("Failed to serve peer connection %s: %v", p.id, err)
		}

		time.Sleep(5 * time.Second)
	}
}
