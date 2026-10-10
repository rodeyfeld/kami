package handlers

import (
	"kami/internal/components/deploys"
	"kami/internal/monitor"
	"kami/internal/server"
	"net/http"

	"github.com/labstack/echo/v4"
)

type DeploysHandler struct {
	server *server.KamiServer
}

func NewDeploysHandler(s *server.KamiServer) *DeploysHandler {
	return &DeploysHandler{server: s}
}

func (h *DeploysHandler) Page(c echo.Context) error {
	return deploys.Page().Render(c.Request().Context(), c.Response().Writer)
}

func (h *DeploysHandler) Partial(c echo.Context) error {
	ctx := c.Request().Context()
	if h.server.Monitor == nil {
		return deploys.Unavailable(monitor.ErrDeploysUnsupported.Error()).Render(ctx, c.Response().Writer)
	}
	d, err := h.server.Monitor.Deploys(ctx)
	if err != nil {
		return deploys.Unavailable(err.Error()).Render(ctx, c.Response().Writer)
	}
	return deploys.Content(d).Render(ctx, c.Response().Writer)
}

// API returns recent rollouts and Woodpecker steps. 503 when kami can't watch the cluster.
func (h *DeploysHandler) API(c echo.Context) error {
	if h.server.Monitor == nil {
		return unavailable(c)
	}
	d, err := h.server.Monitor.Deploys(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, d)
}
