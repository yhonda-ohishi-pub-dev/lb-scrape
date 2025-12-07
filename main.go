package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gorilla/mux"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"lb-scrape/config"
	"lb-scrape/db"
	"lb-scrape/handler"
	grpcserver "lb-scrape/pkg/grpc"
	"lb-scrape/pkg/pb"
	"lb-scrape/service"
)

func main() {
	ctx := context.Background()

	// Load config
	var cfg *config.Config
	var err error

	if config.UseParameterManager() {
		project := config.GetProjectID()
		paramName := config.GetParameterName()
		paramVersion := config.GetParameterVersion()
		cfg, err = config.LoadFromParameterManager(ctx, project, paramName, paramVersion)
		if err != nil {
			log.Fatalf("failed to load config from Parameter Manager: %v", err)
		}
		log.Printf("loaded config from Parameter Manager: %s/%s/%s", project, paramName, paramVersion)
	} else {
		cfg = config.Load()
	}

	// Connect to database
	var database *sql.DB

	if cfg.CloudSQLEnabled {
		database, err = db.ConnectCloudSQL(ctx, cfg.CloudSQLInstance, cfg.DBUser, cfg.DBName)
		if err != nil {
			log.Fatalf("failed to connect to Cloud SQL: %v", err)
		}
		log.Printf("connected to Cloud SQL: %s", cfg.CloudSQLInstance)
	} else {
		database, err = db.Connect(cfg.DSN())
		if err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}
		log.Println("connected to database")
	}
	defer database.Close()

	// Initialize services
	lb := service.NewLoadBalancer(database)
	hc := service.NewHealthChecker(lb, cfg.HealthCheckCacheTTL)
	h := handler.New(lb, hc, cfg)

	// Setup HTTP routes
	r := mux.NewRouter()
	r.HandleFunc("/scrape", h.Scrape).Methods("POST")
	r.HandleFunc("/health", h.Health).Methods("GET")
	r.HandleFunc("/targets/status", h.TargetsStatus).Methods("GET")

	// Create gRPC server
	grpcServer := grpc.NewServer()

	// Register gRPC services
	scraperServer := grpcserver.NewScraperServer(lb, hc, cfg)
	pb.RegisterScraperServiceServer(grpcServer, scraperServer)

	// Register health check service for Cloud Run
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable reflection for debugging
	reflection.Register(grpcServer)

	// Create grpc-web wrapped handler for browser clients
	grpcWebHandler := grpcserver.NewGRPCWebHandler(grpcServer, r, cfg.AllowedOrigins)

	// Create handler that routes gRPC vs grpc-web vs HTTP based on content-type
	combinedHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		contentType := req.Header.Get("Content-Type")
		// Native gRPC requests: HTTP/2 with application/grpc content type
		if req.ProtoMajor == 2 && strings.HasPrefix(contentType, "application/grpc") {
			grpcServer.ServeHTTP(w, req)
			return
		}
		// grpc-web requests: application/grpc-web or preflight CORS
		if strings.HasPrefix(contentType, "application/grpc-web") ||
			(req.Method == "OPTIONS" && req.Header.Get("Access-Control-Request-Headers") != "") {
			grpcWebHandler.ServeHTTP(w, req)
			return
		}
		// Regular HTTP requests
		r.ServeHTTP(w, req)
	})

	// Use h2c to support HTTP/2 cleartext (required for gRPC without TLS)
	h2s := &http2.Server{}
	h2cHandler := h2c.NewHandler(combinedHandler, h2s)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: h2cHandler,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("shutting down server...")
		grpcServer.GracefulStop()
		server.Close()
	}()

	log.Printf("starting lb-scrape on port %s (HTTP + gRPC + grpc-web with h2c)", cfg.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	log.Println("server stopped")
}
