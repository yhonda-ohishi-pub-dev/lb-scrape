package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"

	"lb-scrape/config"
	"lb-scrape/db"
	"lb-scrape/handler"
	"lb-scrape/service"
)

func main() {
	cfg := config.Load()

	// Connect to database
	database, err := db.Connect(cfg.DSN())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Println("connected to database")

	// Initialize services
	lb := service.NewLoadBalancer(database)
	hc := service.NewHealthChecker(lb, cfg.HealthCheckCacheTTL)
	h := handler.New(lb, hc, cfg)

	// Setup routes
	r := mux.NewRouter()
	r.HandleFunc("/scrape", h.Scrape).Methods("POST")
	r.HandleFunc("/health", h.Health).Methods("GET")
	r.HandleFunc("/targets/status", h.TargetsStatus).Methods("GET")

	// Start server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("shutting down server...")
		server.Close()
	}()

	log.Printf("starting lb-scrape on port %s", cfg.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	log.Println("server stopped")
}
