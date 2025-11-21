package main

import (
	application "kami"
	"log"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file for local development
	// In production (Kubernetes), environment variables are set via deployment.yaml
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	application.Start()
}

