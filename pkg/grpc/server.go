package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"google.golang.org/api/idtoken"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"lb-scrape/config"
	"lb-scrape/pkg/pb"
	"lb-scrape/service"
)

// ScraperServer implements pb.ScraperServiceServer
type ScraperServer struct {
	pb.UnimplementedScraperServiceServer
	lb            *service.LoadBalancer
	healthChecker *service.HealthChecker
	cfg           *config.Config
	client        *http.Client
}

// NewScraperServer creates a new gRPC scraper server
func NewScraperServer(lb *service.LoadBalancer, hc *service.HealthChecker, cfg *config.Config) *ScraperServer {
	return &ScraperServer{
		lb:            lb,
		healthChecker: hc,
		cfg:           cfg,
		client: &http.Client{
			Timeout: cfg.VPSRequestTimeout,
		},
	}
}

// Scrape executes a scraping job on the best available target
func (s *ScraperServer) Scrape(ctx context.Context, req *pb.ScrapeRequest) (*pb.ScrapeResponse, error) {
	// Select best target
	target, err := s.lb.SelectTarget()
	if err != nil {
		log.Printf("failed to select target: %v", err)
		return nil, status.Error(codes.Unavailable, "no available targets")
	}

	// Check health (with cache)
	if !s.healthChecker.CheckHealth(&target.Target) {
		log.Printf("target %s is unhealthy", target.Name)
		return nil, status.Error(codes.Unavailable, "selected target is unhealthy")
	}

	// Update job status to running
	if err := s.lb.UpdateJobStatus(req.JobId, "running", &target.ID); err != nil {
		log.Printf("failed to update job status: %v", err)
	}

	// Convert payload to JSON
	var payloadBytes []byte
	if req.Payload != nil {
		payloadBytes, _ = req.Payload.MarshalJSON()
	}

	// Forward request to VPS
	vpsReq := map[string]interface{}{
		"job_type": req.JobType,
		"payload":  json.RawMessage(payloadBytes),
	}
	body, _ := json.Marshal(vpsReq)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", target.URL+"/scrape", bytes.NewReader(body))
	if err != nil {
		errMsg := "failed to create request"
		_ = s.lb.UpdateJobResult(req.JobId, nil, errMsg)
		return nil, status.Error(codes.Internal, errMsg)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Set authentication header
	if s.cfg.VPSBearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.VPSBearerToken)
	} else {
		tokenSource, err := idtoken.NewTokenSource(ctx, target.URL)
		if err != nil {
			log.Printf("failed to create token source: %v", err)
			errMsg := "failed to create IAM token"
			_ = s.lb.UpdateJobResult(req.JobId, nil, errMsg)
			return nil, status.Error(codes.Internal, errMsg)
		}
		token, err := tokenSource.Token()
		if err != nil {
			log.Printf("failed to get ID token: %v", err)
			errMsg := "failed to get IAM token"
			_ = s.lb.UpdateJobResult(req.JobId, nil, errMsg)
			return nil, status.Error(codes.Internal, errMsg)
		}
		httpReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		log.Printf("VPS request failed: %v", err)
		errMsg := "VPS request failed: " + err.Error()
		_ = s.lb.UpdateJobResult(req.JobId, nil, errMsg)
		return nil, status.Error(codes.Internal, errMsg)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		errMsg := "failed to read VPS response"
		_ = s.lb.UpdateJobResult(req.JobId, nil, errMsg)
		return nil, status.Error(codes.Internal, errMsg)
	}

	// Parse VPS response
	var vpsResp struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data,omitempty"`
		Error   string          `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &vpsResp); err != nil {
		// Return raw response if not JSON
		if resp.StatusCode == http.StatusOK {
			_ = s.lb.UpdateJobResult(req.JobId, respBody, "")
			return &pb.ScrapeResponse{
				Success: true,
			}, nil
		}
		_ = s.lb.UpdateJobResult(req.JobId, nil, string(respBody))
		return &pb.ScrapeResponse{
			Success: false,
			Error:   string(respBody),
		}, nil
	}

	// Update job result
	if vpsResp.Success {
		_ = s.lb.UpdateJobResult(req.JobId, vpsResp.Data, "")
	} else {
		_ = s.lb.UpdateJobResult(req.JobId, nil, vpsResp.Error)
	}

	// Convert response data to Struct
	var dataStruct *structpb.Struct
	if len(vpsResp.Data) > 0 {
		var dataMap map[string]interface{}
		if err := json.Unmarshal(vpsResp.Data, &dataMap); err == nil {
			dataStruct, _ = structpb.NewStruct(dataMap)
		}
	}

	return &pb.ScrapeResponse{
		Success: vpsResp.Success,
		Data:    dataStruct,
		Error:   vpsResp.Error,
	}, nil
}

// Health returns the health status of the load balancer
func (s *ScraperServer) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{
		Status: "healthy",
		Time:   timestamppb.Now(),
	}, nil
}

// TargetsStatus returns the status of all VPS targets
func (s *ScraperServer) TargetsStatus(ctx context.Context, req *pb.TargetsStatusRequest) (*pb.TargetsStatusResponse, error) {
	targets, err := s.lb.GetAllTargetsWithLoad()
	if err != nil {
		log.Printf("failed to get targets: %v", err)
		return nil, status.Error(codes.Internal, "failed to get targets")
	}

	// Check health of all targets
	healthStatus := s.healthChecker.CheckAllTargets(targets)

	var result []*pb.TargetStatus
	for _, t := range targets {
		ts := &pb.TargetStatus{
			Id:          t.ID,
			Name:        t.Name,
			Url:         t.URL,
			Healthy:     t.Healthy,
			RunningJobs: int32(t.RunningCount),
			LiveHealthy: healthStatus[t.ID],
		}
		if t.LastChecked.Valid {
			ts.LastChecked = timestamppb.New(t.LastChecked.Time)
		}
		result = append(result, ts)
	}

	return &pb.TargetsStatusResponse{
		Targets: result,
		Time:    timestamppb.New(time.Now().UTC()),
	}, nil
}
