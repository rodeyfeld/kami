package server

import (
	"kami/internal/monitor"

	"github.com/labstack/echo/v4"
)

type KamiServer struct {
	Echo    *echo.Echo
	Monitor monitor.Monitor
}

func NewKamiServer() *KamiServer {
	return &KamiServer{Echo: echo.New()}
}

func (s *KamiServer) Start() error {
	return s.Echo.Start(":8080")
}

