package server

import (
	"kami/internal/monitor"
	"kami/internal/uptime"
	"os"

	"github.com/labstack/echo/v4"
)

type KamiServer struct {
	Echo    *echo.Echo
	Monitor monitor.Monitor
	Uptime  *uptime.Store // nil when kami has nowhere to keep history
}

func NewKamiServer() *KamiServer {
	return &KamiServer{Echo: echo.New()}
}

func (s *KamiServer) Start() error {
	port := os.Getenv("KAMI_PORT")
	if port == "" {
		port = "8080"
	}
	return s.Echo.Start(":" + port)
}
