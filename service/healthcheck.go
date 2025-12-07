package service

import (
	"net/http"
	"sync"
	"time"

	"lb-scrape/models"
)

type HealthChecker struct {
	lb       *LoadBalancer
	cacheTTL time.Duration
	cache    map[int64]*healthCacheEntry
	mu       sync.RWMutex
	client   *http.Client
}

type healthCacheEntry struct {
	healthy   bool
	checkedAt time.Time
}

func NewHealthChecker(lb *LoadBalancer, cacheTTL time.Duration) *HealthChecker {
	return &HealthChecker{
		lb:       lb,
		cacheTTL: cacheTTL,
		cache:    make(map[int64]*healthCacheEntry),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// CheckHealth checks target health with caching
func (hc *HealthChecker) CheckHealth(target *models.Target) bool {
	hc.mu.RLock()
	entry, exists := hc.cache[target.ID]
	hc.mu.RUnlock()

	if exists && time.Since(entry.checkedAt) < hc.cacheTTL {
		return entry.healthy
	}

	healthy := hc.doHealthCheck(target.URL)

	hc.mu.Lock()
	hc.cache[target.ID] = &healthCacheEntry{
		healthy:   healthy,
		checkedAt: time.Now(),
	}
	hc.mu.Unlock()

	// Update DB asynchronously
	go func() {
		_ = hc.lb.UpdateTargetHealth(target.ID, healthy)
	}()

	return healthy
}

func (hc *HealthChecker) doHealthCheck(url string) bool {
	resp, err := hc.client.Get(url + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// CheckAllTargets checks health of all targets and updates cache
func (hc *HealthChecker) CheckAllTargets(targets []models.TargetWithLoad) map[int64]bool {
	results := make(map[int64]bool)
	var wg sync.WaitGroup

	for _, t := range targets {
		wg.Add(1)
		go func(target models.TargetWithLoad) {
			defer wg.Done()
			healthy := hc.CheckHealth(&target.Target)
			hc.mu.Lock()
			results[target.ID] = healthy
			hc.mu.Unlock()
		}(t)
	}

	wg.Wait()
	return results
}
