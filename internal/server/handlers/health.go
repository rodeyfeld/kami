package handlers

import (
	"context"
	"kami/internal/components/health"
	"kami/internal/server"
	"log"

	"github.com/labstack/echo/v4"
)

type HealthHandler struct {
	server *server.KamiServer
}

func NewHealthHandler(s *server.KamiServer) *HealthHandler {
	return &HealthHandler{server: s}
}

func (h *HealthHandler) Index(c echo.Context) error {
	mode := "standalone"
	if h.server.Monitor != nil {
		mode = h.server.Monitor.GetMode()
	}
	return health.Dashboard(mode).Render(c.Request().Context(), c.Response().Writer)
}

func (h *HealthHandler) GetStatus(c echo.Context) error {
	ctx := c.Request().Context()

	if h.server.Monitor == nil {
		return health.StandaloneMode().Render(ctx, c.Response().Writer)
	}

	status, err := h.server.Monitor.GetStatus(ctx)
	if err != nil {
		log.Printf("Error getting status: %v", err)
		return health.ErrorState(err.Error()).Render(ctx, c.Response().Writer)
	}

	return health.StatusCards(status, h.server.Monitor.GetMode(), h.history(ctx)).Render(ctx, c.Response().Writer)
}

// history reads the uptime window, or returns nil when kami isn't keeping one or can't read it
func (h *HealthHandler) history(ctx context.Context) *health.History {
	if h.server.Uptime == nil {
		return nil
	}
	days, err := h.server.Uptime.History(ctx, health.UptimeDays)
	if err != nil {
		log.Printf("Error reading uptime: %v", err)
		return nil
	}
	since, err := h.server.Uptime.Since(ctx)
	if err != nil {
		log.Printf("Error reading uptime: %v", err)
		return nil
	}
	return &health.History{Days: days, Since: since}
}
