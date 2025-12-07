package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"google.golang.org/api/idtoken"

	"lb-scrape/config"
	"lb-scrape/service"
)

type Handler struct {
	lb            *service.LoadBalancer
	healthChecker *service.HealthChecker
	cfg           *config.Config
	client        *http.Client
}

func New(lb *service.LoadBalancer, hc *service.HealthChecker, cfg *config.Config) *Handler {
	return &Handler{
		lb:            lb,
		healthChecker: hc,
		cfg:           cfg,
		client: &http.Client{
			Timeout: cfg.VPSRequestTimeout,
		},
	}
}

// ScrapeRequest is the request body for /scrape endpoint
type ScrapeRequest struct {
	JobID   int64           `json:"job_id"`
	JobType string          `json:"job_type"`
	Payload json.RawMessage `json:"payload"`
}

// ScrapeResponse is the response from VPS
type ScrapeResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Scrape handles POST /scrape - main load balancing endpoint
func (h *Handler) Scrape(w http.ResponseWriter, r *http.Request) {
	var req ScrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// Select best target
	target, err := h.lb.SelectTarget()
	if err != nil {
		log.Printf("failed to select target: %v", err)
		http.Error(w, `{"error":"no available targets"}`, http.StatusServiceUnavailable)
		return
	}

	// Check health (with cache)
	if !h.healthChecker.CheckHealth(&target.Target) {
		log.Printf("target %s is unhealthy", target.Name)
		http.Error(w, `{"error":"selected target is unhealthy"}`, http.StatusServiceUnavailable)
		return
	}

	// Update job status to running
	if err := h.lb.UpdateJobStatus(req.JobID, "running", &target.ID); err != nil {
		log.Printf("failed to update job status: %v", err)
	}

	// Forward request to VPS
	vpsReq := map[string]interface{}{
		"job_type": req.JobType,
		"payload":  req.Payload,
	}
	body, _ := json.Marshal(vpsReq)

	httpReq, err := http.NewRequest("POST", target.URL+"/scrape", bytes.NewReader(body))
	if err != nil {
		h.handleVPSError(w, req.JobID, "failed to create request")
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Set authentication header
	if h.cfg.VPSBearerToken != "" {
		// Use static bearer token if configured
		httpReq.Header.Set("Authorization", "Bearer "+h.cfg.VPSBearerToken)
	} else {
		// Use IAM authentication (ID token)
		ctx := context.Background()
		tokenSource, err := idtoken.NewTokenSource(ctx, target.URL)
		if err != nil {
			log.Printf("failed to create token source: %v", err)
			h.handleVPSError(w, req.JobID, "failed to create IAM token")
			return
		}
		token, err := tokenSource.Token()
		if err != nil {
			log.Printf("failed to get ID token: %v", err)
			h.handleVPSError(w, req.JobID, "failed to get IAM token")
			return
		}
		httpReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}

	resp, err := h.client.Do(httpReq)
	if err != nil {
		log.Printf("VPS request failed: %v", err)
		h.handleVPSError(w, req.JobID, "VPS request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.handleVPSError(w, req.JobID, "failed to read VPS response")
		return
	}

	// Parse VPS response
	var vpsResp ScrapeResponse
	if err := json.Unmarshal(respBody, &vpsResp); err != nil {
		// Return raw response if not JSON
		if resp.StatusCode == http.StatusOK {
			_ = h.lb.UpdateJobResult(req.JobID, respBody, "")
		} else {
			_ = h.lb.UpdateJobResult(req.JobID, nil, string(respBody))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// Update job result
	if vpsResp.Success {
		_ = h.lb.UpdateJobResult(req.JobID, vpsResp.Data, "")
	} else {
		_ = h.lb.UpdateJobResult(req.JobID, nil, vpsResp.Error)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	json.NewEncoder(w).Encode(vpsResp)
}

func (h *Handler) handleVPSError(w http.ResponseWriter, jobID int64, errMsg string) {
	_ = h.lb.UpdateJobResult(jobID, nil, errMsg)
	http.Error(w, `{"error":"`+errMsg+`"}`, http.StatusBadGateway)
}

// Health handles GET /health - LB health check
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// TargetsStatus handles GET /targets/status - monitoring endpoint
func (h *Handler) TargetsStatus(w http.ResponseWriter, r *http.Request) {
	targets, err := h.lb.GetAllTargetsWithLoad()
	if err != nil {
		log.Printf("failed to get targets: %v", err)
		http.Error(w, `{"error":"failed to get targets"}`, http.StatusInternalServerError)
		return
	}

	// Check health of all targets
	healthStatus := h.healthChecker.CheckAllTargets(targets)

	type TargetStatus struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		URL          string `json:"url"`
		Healthy      bool   `json:"healthy"`
		RunningJobs  int    `json:"running_jobs"`
		LastChecked  string `json:"last_checked,omitempty"`
		LiveHealthy  bool   `json:"live_healthy"`
	}

	var result []TargetStatus
	for _, t := range targets {
		ts := TargetStatus{
			ID:          t.ID,
			Name:        t.Name,
			URL:         t.URL,
			Healthy:     t.Healthy,
			RunningJobs: t.RunningCount,
			LiveHealthy: healthStatus[t.ID],
		}
		if t.LastChecked.Valid {
			ts.LastChecked = t.LastChecked.Time.Format(time.RFC3339)
		}
		result = append(result, ts)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"targets": result,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}
