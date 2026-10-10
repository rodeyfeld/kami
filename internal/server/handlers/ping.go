package handlers

import (
	"errors"
	"kami/internal/k8s"
	"kami/internal/server"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

type PingHandler struct {
	server *server.KamiServer
}

func NewPingHandler(s *server.KamiServer) *PingHandler {
	return &PingHandler{server: s}
}

// Ping checks one workload. Responds 200 when it's healthy and 503 when it isn't,
// so callers can use the status code alone or read the JSON body for details.
func (h *PingHandler) Ping(c echo.Context) error {
	if h.server.Monitor == nil {
		return unavailable(c)
	}

	result, err := h.server.Monitor.Ping(c.Request().Context(), c.Param("name"))
	if errors.Is(err, k8s.ErrWorkloadNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	if err != nil {
		log.Printf("Error pinging %s: %v", c.Param("name"), err)
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}

	code := http.StatusOK
	if !result.OK {
		code = http.StatusServiceUnavailable
	}
	return c.JSON(code, result)
}

// PingAll checks every Deployment and StatefulSet. Always 200 if kami could reach the cluster; check each result's ok.
func (h *PingHandler) PingAll(c echo.Context) error {
	if h.server.Monitor == nil {
		return unavailable(c)
	}

	results, err := h.server.Monitor.PingAll(c.Request().Context())
	if err != nil {
		log.Printf("Error pinging workloads: %v", err)
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, results)
}

func unavailable(c echo.Context) error {
	return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "no monitoring backend"})
}
