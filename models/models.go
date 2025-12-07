package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

type Target struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	URL         string         `json:"url"`
	Healthy     bool           `json:"healthy"`
	LastChecked sql.NullTime   `json:"last_checked"`
}

type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

type Job struct {
	ID           int64           `json:"id"`
	JobType      string          `json:"job_type"`
	Payload      json.RawMessage `json:"payload"`
	Status       JobStatus       `json:"status"`
	TargetID     sql.NullInt64   `json:"target_id"`
	CreatedAt    time.Time       `json:"created_at"`
	StartedAt    sql.NullTime    `json:"started_at"`
	CompletedAt  sql.NullTime    `json:"completed_at"`
	Result       json.RawMessage `json:"result"`
	ErrorMessage sql.NullString  `json:"error_message"`
	RetryCount   int             `json:"retry_count"`
}

type TargetWithLoad struct {
	Target
	RunningCount int `json:"running_count"`
}
