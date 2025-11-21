package application

import (
	"kami/internal/server"
	"kami/internal/server/routes"
	"kami/internal/startup"
	"log"
)

func Start() {
	s := server.NewKamiServer()
	s.Monitor = startup.InitMonitor()

	s.Echo.Static("/static", "static")
	routes.Setup(s)

	if err := s.Start(); err != nil {
		log.Fatal(err)
	}
}
