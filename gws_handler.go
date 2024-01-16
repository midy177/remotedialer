package remotedialer

import (
	"github.com/lxzan/gws"
	"github.com/sirupsen/logrus"
	"net/http"
)

type ServerOption func() (option *gws.ServerOption)
type ClientOption func(addr string, headers http.Header) (option *gws.ClientOption)

var DefaultServerOption ServerOption = func() (option *gws.ServerOption) {
	return &gws.ServerOption{
		// Asynchronous reading needs to be turned off, otherwise message order problems will occur.
		ReadAsyncEnabled: false,        // Parallel message processing
		CompressEnabled:  false,        // Enable compression
		Recovery:         gws.Recovery, // Exception recovery
		Logger:           logrus.StandardLogger(),
		NewSession: func() gws.SessionStorage {
			return newSMap()
		},
	}
}

var DefaultClientOption ClientOption = func(addr string, headers http.Header) (option *gws.ClientOption) {
	return &gws.ClientOption{
		// Asynchronous reading needs to be turned off, otherwise message order problems will occur.
		ReadAsyncEnabled: false,        // Parallel message processing
		CompressEnabled:  false,        // Enable compression
		Recovery:         gws.Recovery, // Exception recovery
		Addr:             addr,
		RequestHeader:    headers,
		Logger:           logrus.StandardLogger(),
		NewSession: func() gws.SessionStorage {
			return newSMap()
		},
	}
}
