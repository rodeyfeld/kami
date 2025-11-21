package handlers

import (
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

	return health.StatusCards(status, h.server.Monitor.GetMode()).Render(ctx, c.Response().Writer)
}
