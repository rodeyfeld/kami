package routes

import (
	"kami/internal/server"
	"kami/internal/server/handlers"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func Setup(s *server.KamiServer) {
	s.Echo.Use(middleware.Logger(), middleware.Recover())

	h := handlers.NewHealthHandler(s)
	s.Echo.GET("/", h.Index)
	s.Echo.GET("/partials/status", h.GetStatus)

	d := handlers.NewDeploysHandler(s)
	s.Echo.GET("/deploys", d.Page)
	s.Echo.GET("/partials/deploys", d.Partial)

	// JSON API for other services, e.g. status dots on pinwheel.fan
	p := handlers.NewPingHandler(s)
	api := s.Echo.Group("/api", middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet},
	}))
	api.GET("/ping", p.PingAll)
	api.GET("/ping/:name", p.Ping)
	api.GET("/deploys", d.API)

	// Health check endpoints (Kubernetes best practice)
	s.Echo.GET("/livez", func(c echo.Context) error { return c.String(200, "ok") })
	s.Echo.GET("/readyz", func(c echo.Context) error { return c.String(200, "ok") })
}
