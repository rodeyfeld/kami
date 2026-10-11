package application

import (
	"context"
	"kami/internal/server"
	"kami/internal/server/routes"
	"kami/internal/startup"
	"kami/internal/uptime"
	"log"
	"os"
	"time"
)

func Start() {
	s := server.NewKamiServer()
	s.Monitor = startup.InitMonitor()
	s.Uptime = openUptime()
	if s.Monitor != nil && s.Uptime != nil {
		go s.Uptime.Run(context.Background(), func(ctx context.Context) ([]uptime.Sample, error) {
			status, err := s.Monitor.GetStatus(ctx)
			if err != nil {
				return nil, err
			}
			return status.Samples(), nil
		}, time.Minute)
	}

	s.Echo.Static("/static", "static")
	routes.Setup(s)

	if err := s.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// openUptime opens the history database, which lives on kami's PVC in the cluster. Without
// it the health page still works, just without uptime.
func openUptime() *uptime.Store {
	path := os.Getenv("KAMI_UPTIME_DB")
	if path == "" {
		path = "/data/kami.db"
	}
	store, err := uptime.Open(path)
	if err != nil {
		log.Printf("⚠ No uptime history: %v", err)
		return nil
	}
	log.Printf("✓ Uptime history in %s", path)
	return store
}
