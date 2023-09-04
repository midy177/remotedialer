package main

import (
	"context"
	"flag"
	"github.com/labstack/echo/v4"
	"net/http"

	"github.com/rancher/remotedialer"
	"github.com/sirupsen/logrus"
)

var (
	addr  string
	id    string
	debug bool
)

func main() {
	flag.StringVar(&addr, "connect", "ws://localhost:8123/connect", "Address to connect to")
	flag.StringVar(&id, "id", "foo", "Client ID")
	flag.BoolVar(&debug, "debug", true, "Debug logging")
	flag.Parse()

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	headers := http.Header{
		"X-Tunnel-ID": []string{id},
	}
	remotedialer.ClientRouter.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	remotedialer.ClientConnect(context.Background(), addr, headers, nil, func(string, string) bool { return true }, nil)
}
