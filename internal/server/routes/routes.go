package routes

import (
	"kami/internal/server"
	"kami/internal/server/handlers"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func Setup(s *server.KamiServer) {
	s.Echo.Use(middleware.Logger(), middleware.Recover())

	h := handlers.NewHealthHandler(s)
	s.Echo.GET("/", h.Index)
	s.Echo.GET("/api/status", h.GetStatus)

	// Health check endpoints (Kubernetes best practice)
	s.Echo.GET("/livez", func(c echo.Context) error { return c.String(200, "ok") })
	s.Echo.GET("/readyz", func(c echo.Context) error { return c.String(200, "ok") })
}
