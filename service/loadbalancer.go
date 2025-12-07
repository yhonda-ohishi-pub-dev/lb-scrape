package service

import (
	"database/sql"
	"time"

	"lb-scrape/models"
)

type LoadBalancer struct {
	db *sql.DB
}

func NewLoadBalancer(db *sql.DB) *LoadBalancer {
	return &LoadBalancer{db: db}
}

// SelectTarget selects the healthiest VPS with the least running jobs
func (lb *LoadBalancer) SelectTarget() (*models.TargetWithLoad, error) {
	query := `
		SELECT
			t.id,
			t.name,
			t.url,
			t.healthy,
			t.last_checked,
			COUNT(j.id) as running_count
		FROM scraper_targets t
		LEFT JOIN scraper_jobs j
			ON t.id = j.target_id AND j.status = 'running'
		WHERE t.healthy = TRUE
		GROUP BY t.id, t.name, t.url, t.healthy, t.last_checked
		ORDER BY running_count ASC, t.id ASC
		LIMIT 1
	`

	var target models.TargetWithLoad
	err := lb.db.QueryRow(query).Scan(
		&target.ID,
		&target.Name,
		&target.URL,
		&target.Healthy,
		&target.LastChecked,
		&target.RunningCount,
	)
	if err != nil {
		return nil, err
	}

	return &target, nil
}

// GetAllTargetsWithLoad returns all targets with their current load
func (lb *LoadBalancer) GetAllTargetsWithLoad() ([]models.TargetWithLoad, error) {
	query := `
		SELECT
			t.id,
			t.name,
			t.url,
			t.healthy,
			t.last_checked,
			COUNT(j.id) as running_count
		FROM scraper_targets t
		LEFT JOIN scraper_jobs j
			ON t.id = j.target_id AND j.status = 'running'
		GROUP BY t.id, t.name, t.url, t.healthy, t.last_checked
		ORDER BY t.id ASC
	`

	rows, err := lb.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []models.TargetWithLoad
	for rows.Next() {
		var t models.TargetWithLoad
		if err := rows.Scan(&t.ID, &t.Name, &t.URL, &t.Healthy, &t.LastChecked, &t.RunningCount); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}

	return targets, nil
}

// UpdateJobStatus updates job status with timestamps
func (lb *LoadBalancer) UpdateJobStatus(jobID int64, status models.JobStatus, targetID *int64) error {
	var query string
	var args []interface{}

	switch status {
	case models.JobStatusRunning:
		query = `UPDATE scraper_jobs SET status = $1, target_id = $2, started_at = $3 WHERE id = $4`
		args = []interface{}{status, targetID, time.Now(), jobID}
	case models.JobStatusCompleted, models.JobStatusFailed:
		query = `UPDATE scraper_jobs SET status = $1, completed_at = $2 WHERE id = $3`
		args = []interface{}{status, time.Now(), jobID}
	default:
		query = `UPDATE scraper_jobs SET status = $1 WHERE id = $2`
		args = []interface{}{status, jobID}
	}

	_, err := lb.db.Exec(query, args...)
	return err
}

// UpdateJobResult updates job with result or error
func (lb *LoadBalancer) UpdateJobResult(jobID int64, result []byte, errMsg string) error {
	if errMsg != "" {
		query := `UPDATE scraper_jobs SET status = $1, completed_at = $2, error_message = $3 WHERE id = $4`
		_, err := lb.db.Exec(query, models.JobStatusFailed, time.Now(), errMsg, jobID)
		return err
	}

	query := `UPDATE scraper_jobs SET status = $1, completed_at = $2, result = $3 WHERE id = $4`
	_, err := lb.db.Exec(query, models.JobStatusCompleted, time.Now(), result, jobID)
	return err
}

// UpdateTargetHealth updates health status of a target
func (lb *LoadBalancer) UpdateTargetHealth(targetID int64, healthy bool) error {
	query := `UPDATE scraper_targets SET healthy = $1, last_checked = $2 WHERE id = $3`
	_, err := lb.db.Exec(query, healthy, time.Now(), targetID)
	return err
}

// GetTarget returns a target by ID
func (lb *LoadBalancer) GetTarget(targetID int64) (*models.Target, error) {
	query := `SELECT id, name, url, healthy, last_checked FROM scraper_targets WHERE id = $1`
	var t models.Target
	err := lb.db.QueryRow(query, targetID).Scan(&t.ID, &t.Name, &t.URL, &t.Healthy, &t.LastChecked)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
