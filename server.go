package remotedialer

import (
	"github.com/lxzan/gws"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"math/rand"
	"net/http"
	"sync"
)

var (
	errFailedAuth       = errors.New("failed authentication")
	errWrongMessageType = errors.New("wrong websocket message type")
)

type Authorizer func(req *http.Request) (clientKey string, authed bool, err error)
type ErrorWriter func(rw http.ResponseWriter, req *http.Request, code int, err error)

var DefaultErrorWriter = func(rw http.ResponseWriter, req *http.Request, code int, err error) {
	rw.WriteHeader(code)
	_, _ = rw.Write([]byte(err.Error()))
}

type Server struct {
	PeerID                  string
	PeerToken               string
	ClientConnectAuthorizer ConnectAuthorizer
	authorizer              Authorizer
	sessions                *sessionManager
	peers                   map[string]*peer
	peerLock                sync.Mutex
}

func New(auth Authorizer) *Server {
	return &Server{
		peers:      map[string]*peer{},
		authorizer: auth,
		sessions:   newSessionManager(),
	}
}

func (s *Server) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	clientKey, authed, p, err := s.auth(req)
	if err != nil {
		DefaultErrorWriter(rw, req, 400, err)
		return
	}
	if !authed {
		DefaultErrorWriter(rw, req, 401, errFailedAuth)
		return
	}
	sessionKey := rand.Int63()
	session := newSessionWithoutConn(sessionKey, clientKey)
	defer session.Close()
	upgrader := gws.NewUpgrader(session, DefaultServerOption())
	conn, err := upgrader.Upgrade(rw, req)
	if err != nil {
		return
	}
	logrus.Infof("Handling backend connection request [%s]", clientKey)
	s.sessions.add(clientKey, session, p)
	session.auth = s.ClientConnectAuthorizer
	defer s.sessions.remove(session)
	conn.ReadLoop()
}

func (s *Server) auth(req *http.Request) (clientKey string, authed, peer bool, err error) {
	id := req.Header.Get(ID)
	token := req.Header.Get(Token)
	if id != "" && token != "" {
		// peer authentication
		s.peerLock.Lock()
		p, ok := s.peers[id]
		s.peerLock.Unlock()

		if ok && p.token == token {
			return id, true, true, nil
		}
	}

	id, authed, err = s.authorizer(req)
	return id, authed, false, err
}
