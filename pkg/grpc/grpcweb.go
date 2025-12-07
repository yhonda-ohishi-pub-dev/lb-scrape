package grpc

import (
	"net/http"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"google.golang.org/grpc"
)

// WrapWithGRPCWeb wraps a gRPC server to support grpc-web protocol
func WrapWithGRPCWeb(grpcServer *grpc.Server, allowedOrigins []string) http.Handler {
	options := []grpcweb.Option{
		grpcweb.WithCorsForRegisteredEndpointsOnly(false),
		grpcweb.WithOriginFunc(func(origin string) bool {
			// Allow all origins if no specific origins configured
			if len(allowedOrigins) == 0 {
				return true
			}
			for _, allowed := range allowedOrigins {
				if allowed == "*" || allowed == origin {
					return true
				}
			}
			return false
		}),
		grpcweb.WithAllowedRequestHeaders([]string{
			"Accept",
			"Accept-Language",
			"Content-Language",
			"Content-Type",
			"Authorization",
			"X-Grpc-Web",
			"X-User-Agent",
		}),
	}

	wrappedGrpc := grpcweb.WrapServer(grpcServer, options...)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wrappedGrpc.IsGrpcWebRequest(r) || wrappedGrpc.IsAcceptableGrpcCorsRequest(r) {
			wrappedGrpc.ServeHTTP(w, r)
			return
		}
		// Fallback to regular HTTP handler if needed
		http.NotFound(w, r)
	})
}

// NewGRPCWebHandler creates a combined HTTP handler that serves both grpc-web and regular HTTP
func NewGRPCWebHandler(grpcServer *grpc.Server, httpHandler http.Handler, allowedOrigins []string) http.Handler {
	options := []grpcweb.Option{
		grpcweb.WithCorsForRegisteredEndpointsOnly(false),
		grpcweb.WithOriginFunc(func(origin string) bool {
			if len(allowedOrigins) == 0 {
				return true
			}
			for _, allowed := range allowedOrigins {
				if allowed == "*" || allowed == origin {
					return true
				}
			}
			return false
		}),
		grpcweb.WithAllowedRequestHeaders([]string{
			"Accept",
			"Accept-Language",
			"Content-Language",
			"Content-Type",
			"Authorization",
			"X-Grpc-Web",
			"X-User-Agent",
		}),
	}

	wrappedGrpc := grpcweb.WrapServer(grpcServer, options...)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle grpc-web requests
		if wrappedGrpc.IsGrpcWebRequest(r) || wrappedGrpc.IsAcceptableGrpcCorsRequest(r) {
			wrappedGrpc.ServeHTTP(w, r)
			return
		}
		// Fallback to regular HTTP handler
		httpHandler.ServeHTTP(w, r)
	})
}
