package remotedialer

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	Data messageType = iota + 1
	Connect
	Error
	AddClient
	RemoveClient
	Pause
	Resume
)

var (
	idCounter      int64
	legacyDeadline = (15 * time.Second).Milliseconds()
	messagePool    sync.Pool
)

func init() {
	r := rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	idCounter = r.Int63()
	messagePool = sync.Pool{
		New: func() any {
			return &message{}
		},
	}
}

type messageType int64

type message struct {
	id          int64
	err         error
	connID      int64
	messageType messageType
	bytes       []byte
	body        io.Reader
	proto       string
	address     string
}

func nextId() int64 {
	return atomic.AddInt64(&idCounter, 1)
}

func newDataMessage(connID int64, bytes []byte) *message {
	msg := messagePool.Get().(*message)
	msg.id = nextId()
	msg.connID = connID
	msg.messageType = Data
	msg.bytes = bytes
	return msg
}

func newPauseMessage(connID int64) *message {
	msg := messagePool.Get().(*message)
	msg.id = nextId()
	msg.connID = connID
	msg.messageType = Pause
	return msg
}

func newResumeMessage(connID int64) *message {
	msg := messagePool.Get().(*message)
	msg.id = nextId()
	msg.connID = connID
	msg.messageType = Resume
	return msg
}

func newConnectMessage(connID int64, proto, address string) *message {
	msg := messagePool.Get().(*message)
	msg.id = nextId()
	msg.connID = connID
	msg.messageType = Connect
	msg.bytes = []byte(fmt.Sprintf("%s/%s", proto, address))
	msg.proto = proto
	msg.address = address
	return msg
}

func newErrorMessage(connID int64, err error) *message {
	msg := messagePool.Get().(*message)
	msg.id = nextId()
	msg.connID = connID
	msg.messageType = Error
	msg.bytes = []byte(err.Error())
	return msg
}

func newAddClient(client string) *message {
	msg := messagePool.Get().(*message)
	msg.id = nextId()
	msg.messageType = AddClient
	msg.bytes = []byte(client)
	return msg
}

func newRemoveClient(client string) *message {
	return &message{
		id:          nextId(),
		messageType: RemoveClient,
		address:     client,
		bytes:       []byte(client),
	}
}

func newServerMessage(reader io.Reader) (*message, error) {
	buf := bufio.NewReader(reader)

	id, err := binary.ReadVarint(buf)
	if err != nil {
		return nil, err
	}

	connID, err := binary.ReadVarint(buf)
	if err != nil {
		return nil, err
	}

	mType, err := binary.ReadVarint(buf)
	if err != nil {
		return nil, err
	}

	m := messagePool.Get().(*message)
	m.id = id
	m.connID = connID
	m.messageType = messageType(mType)
	m.body = buf

	if m.messageType == Data || m.messageType == Connect {
		// no longer used, this is the deadline field
		_, err := binary.ReadVarint(buf)
		if err != nil {
			return nil, err
		}
	}
	if m.messageType == Connect {
		bytes, err := io.ReadAll(io.LimitReader(buf, 512))
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(string(bytes), "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("failed to parse connect address")
		}
		m.proto = parts[0]
		m.address = parts[1]
		m.bytes = bytes
	} else if m.messageType == AddClient || m.messageType == RemoveClient {
		bytes, err := io.ReadAll(io.LimitReader(buf, 100))
		if err != nil {
			return nil, err
		}
		m.address = string(bytes)
		m.bytes = bytes
	}

	return m, nil
}

func (m *message) put() {
	m.err = nil
	m.bytes = nil
	m.body = nil
	m.proto = ""
	m.address = ""
	messagePool.Put(m)
}

func (m *message) Err() error {
	if m.err != nil {
		return m.err
	}
	bytes, err := io.ReadAll(io.LimitReader(m.body, 100))
	if err != nil {
		return err
	}

	str := string(bytes)
	if str == "EOF" {
		m.err = io.EOF
	} else {
		m.err = errors.New(str)
	}
	return m.err
}

func (m *message) BytesLength() int {
	space := len(m.bytes) + 24
	if m.messageType == Data || m.messageType == Connect {
		// no longer used, this is the deadline field
		space += 8
	}

	return space
}

func (m *message) Bytes() []byte {
	// Calculate required buffer size
	space := len(m.bytes) + 24
	if m.messageType == Data || m.messageType == Connect {
		// no longer used, this is the deadline field
		space += 8
	}
	buf := make([]byte, space)
	// offset of header data
	offset := m.header(buf)
	// Copy message data to buffer
	copy(buf[offset:], m.bytes)
	return buf
}

func (m *message) header(buf []byte) int {
	// offset of header data
	offset := 0
	//Write various header information into the buffer
	offset += binary.PutVarint(buf[offset:], m.id)
	offset += binary.PutVarint(buf[offset:], m.connID)
	offset += binary.PutVarint(buf[offset:], int64(m.messageType))
	if m.messageType == Data || m.messageType == Connect {
		offset += binary.PutVarint(buf[offset:], legacyDeadline)
	}
	return offset
}

func (m *message) Read(p []byte) (int, error) {
	return m.body.Read(p)
}

func (m *message) WriteTo(deadline time.Time, wsConn *wsConn) (int, error) {
	err := wsConn.WriteMessage(websocket.BinaryMessage, deadline, m.Bytes())
	return len(m.bytes), err
}

func (m *message) String() string {
	switch m.messageType {
	case Data:
		if m.body == nil {
			return fmt.Sprintf("%d DATA         [%d]: %d bytes: %s", m.id, m.connID, len(m.bytes), string(m.bytes))
		}
		return fmt.Sprintf("%d DATA         [%d]: buffered", m.id, m.connID)
	case Error:
		return fmt.Sprintf("%d ERROR        [%d]: %s", m.id, m.connID, m.Err())
	case Connect:
		return fmt.Sprintf("%d CONNECT      [%d]: %s/%s", m.id, m.connID, m.proto, m.address)
	case AddClient:
		return fmt.Sprintf("%d ADDCLIENT    [%s]", m.id, m.address)
	case RemoveClient:
		return fmt.Sprintf("%d REMOVECLIENT [%s]", m.id, m.address)
	case Pause:
		return fmt.Sprintf("%d PAUSE        [%d]", m.id, m.connID)
	case Resume:
		return fmt.Sprintf("%d RESUME       [%d]", m.id, m.connID)
	default:
		return fmt.Sprintf("%d UNKNOWN[%d]: %d", m.id, m.connID, m.messageType)
	}
}
