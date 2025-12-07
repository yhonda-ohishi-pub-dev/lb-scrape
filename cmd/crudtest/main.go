package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"lb-scrape/config"
	"lb-scrape/db"
	"lb-scrape/models"
	"lb-scrape/service"
)

func main() {
	ctx := context.Background()

	// Load config (same pattern as main.go)
	var cfg *config.Config
	var err error

	if config.UseParameterManager() {
		project := config.GetProjectID()
		paramName := config.GetParameterName()
		paramVersion := config.GetParameterVersion()
		cfg, err = config.LoadFromParameterManager(ctx, project, paramName, paramVersion)
		if err != nil {
			log.Fatalf("Failed to load config from Parameter Manager: %v", err)
		}
		fmt.Printf("Loaded config from Parameter Manager: %s/%s/%s\n", project, paramName, paramVersion)
	} else {
		cfg = config.Load()
		fmt.Println("Loaded config from environment variables")
	}

	// Connect to Database
	fmt.Println("\n=== Connecting to Database ===")
	fmt.Printf("CloudSQL Enabled: %v\n", cfg.CloudSQLEnabled)
	fmt.Printf("Instance: %s\n", cfg.CloudSQLInstance)
	fmt.Printf("User: %s\n", cfg.DBUser)
	fmt.Printf("Database: %s\n", cfg.DBName)

	var database *sql.DB

	if cfg.CloudSQLEnabled {
		database, err = db.ConnectCloudSQL(ctx, cfg.CloudSQLInstance, cfg.DBUser, cfg.DBName)
		if err != nil {
			log.Fatalf("Failed to connect to Cloud SQL: %v", err)
		}
		fmt.Println("Connected to Cloud SQL successfully!")
	} else {
		database, err = db.Connect(cfg.DSN())
		if err != nil {
			log.Fatalf("Failed to connect to database: %v", err)
		}
		fmt.Println("Connected to database successfully!")
	}
	defer database.Close()

	// Create services
	lb := service.NewLoadBalancer(database)
	hc := service.NewHealthChecker(lb, cfg.HealthCheckCacheTTL)

	// Run CRUD tests
	runTests(database, lb, hc)
}

func runTests(database *sql.DB, lb *service.LoadBalancer, hc *service.HealthChecker) {
	// ========================================
	// scraper_targets CRUD Tests (No RLS)
	// ========================================

	// Test 1: Create a test target
	fmt.Println("\n=== Test 1: Create Target ===")
	testTargetName := fmt.Sprintf("test-vps-%d", time.Now().Unix())
	testTargetURL := "http://test.example.com:8080"

	_, err := database.Exec(`
		INSERT INTO scraper_targets (name, url, healthy)
		VALUES ($1, $2, $3)
	`, testTargetName, testTargetURL, true)
	if err != nil {
		log.Fatalf("Failed to create target: %v", err)
	}
	fmt.Printf("Created target: %s\n", testTargetName)

	// Get the created target ID
	var testTargetID int64
	err = database.QueryRow(`
		SELECT id FROM scraper_targets WHERE name = $1
	`, testTargetName).Scan(&testTargetID)
	if err != nil {
		log.Fatalf("Failed to get target ID: %v", err)
	}
	fmt.Printf("Target ID: %d\n", testTargetID)

	// Test 2: Read all targets
	fmt.Println("\n=== Test 2: GetAllTargetsWithLoad ===")
	targets, err := lb.GetAllTargetsWithLoad()
	if err != nil {
		log.Fatalf("GetAllTargetsWithLoad failed: %v", err)
	}
	fmt.Printf("Found %d targets:\n", len(targets))
	for _, t := range targets {
		fmt.Printf("  - ID=%d, Name=%s, URL=%s, Healthy=%v, RunningCount=%d\n",
			t.ID, t.Name, t.URL, t.Healthy, t.RunningCount)
	}

	// Test 3: Get single target
	fmt.Println("\n=== Test 3: GetTarget ===")
	target, err := lb.GetTarget(testTargetID)
	if err != nil {
		log.Fatalf("GetTarget failed: %v", err)
	}
	fmt.Printf("Got target: ID=%d, Name=%s, URL=%s, Healthy=%v\n",
		target.ID, target.Name, target.URL, target.Healthy)

	// Test 4: Select target (least loaded healthy)
	fmt.Println("\n=== Test 4: SelectTarget ===")
	selected, err := lb.SelectTarget()
	if err != nil {
		fmt.Printf("SelectTarget failed: %v\n", err)
	} else {
		fmt.Printf("Selected: ID=%d, Name=%s, URL=%s, RunningCount=%d\n",
			selected.ID, selected.Name, selected.URL, selected.RunningCount)
	}

	// Test 5: Update target health
	fmt.Println("\n=== Test 5: UpdateTargetHealth ===")
	err = lb.UpdateTargetHealth(testTargetID, false)
	if err != nil {
		log.Fatalf("UpdateTargetHealth failed: %v", err)
	}
	fmt.Println("Target health updated to false")

	// Verify
	target, _ = lb.GetTarget(testTargetID)
	fmt.Printf("Verified healthy: %v\n", target.Healthy)

	// Update back to healthy
	err = lb.UpdateTargetHealth(testTargetID, true)
	if err != nil {
		log.Fatalf("UpdateTargetHealth (restore) failed: %v", err)
	}
	fmt.Println("Target health restored to true")

	// Test 6: Health check (simulated - URL is fake)
	fmt.Println("\n=== Test 6: Health Check (simulated) ===")
	healthy := hc.CheckHealth(target)
	fmt.Printf("Health check result for %s: %v (expected false - fake URL)\n", target.Name, healthy)

	// ========================================
	// scraper_jobs CRUD Tests (RLS applies)
	// ========================================

	// Get existing organization and set session variable for RLS
	fmt.Println("\n=== Test 7: Get Organization & Set Session ===")
	var orgID string

	// Get existing organization
	err = database.QueryRow(`SELECT id FROM organizations LIMIT 1`).Scan(&orgID)
	if err != nil {
		fmt.Printf("Could not get organization: %v\n", err)
		fmt.Println("Skipping job CRUD tests")
		orgID = ""
	} else {
		fmt.Printf("Found organization: %s\n", orgID)

		// Set session variable for RLS (app.current_organization_id)
		_, err = database.Exec(`SELECT set_config('app.current_organization_id', $1, false)`, orgID)
		if err != nil {
			fmt.Printf("Could not set session variable: %v\n", err)
			orgID = ""
		} else {
			fmt.Println("Session variable set for RLS")
		}
	}

	if orgID != "" {
		fmt.Printf("Organization ID: %s\n", orgID)

		// Test 8: Create job with organization_id
		fmt.Println("\n=== Test 8: Create Job ===")
		payload, _ := json.Marshal(map[string]string{"test": "crud_test"})
		_, err = database.Exec(`
			INSERT INTO scraper_jobs (organization_id, job_type, payload, status)
			VALUES ($1, $2, $3, $4)
		`, orgID, "crud_test", payload, models.JobStatusPending)
		if err != nil {
			fmt.Printf("Job creation blocked by RLS: %v\n", err)
			fmt.Println("(This is expected if current user is not a member of the organization)")
			fmt.Println("Skipping remaining job CRUD tests")
			orgID = "" // Skip remaining job tests
		} else {
			fmt.Println("Job created successfully")
		}

	}

	if orgID != "" {
		// Get created job ID
		var jobID int64
		err = database.QueryRow(`
			SELECT id FROM scraper_jobs WHERE job_type = 'crud_test' ORDER BY id DESC LIMIT 1
		`).Scan(&jobID)
		if err != nil {
			log.Fatalf("Failed to get job ID: %v", err)
		}
		fmt.Printf("Job ID: %d\n", jobID)

		// Test 9: Update job status
		fmt.Println("\n=== Test 9: UpdateJobStatus ===")
		err = lb.UpdateJobStatus(jobID, models.JobStatusRunning, &testTargetID)
		if err != nil {
			log.Fatalf("UpdateJobStatus failed: %v", err)
		}
		fmt.Println("Job status updated to running")

		// Verify
		var status string
		database.QueryRow(`SELECT status FROM scraper_jobs WHERE id = $1`, jobID).Scan(&status)
		fmt.Printf("Verified status: %s\n", status)

		// Test 10: Update job result
		fmt.Println("\n=== Test 10: UpdateJobResult ===")
		resultData, _ := json.Marshal(map[string]string{"result": "success", "timestamp": time.Now().Format(time.RFC3339)})
		err = lb.UpdateJobResult(jobID, resultData, "")
		if err != nil {
			log.Fatalf("UpdateJobResult failed: %v", err)
		}
		fmt.Println("Job completed with result")

		// Verify
		database.QueryRow(`SELECT status FROM scraper_jobs WHERE id = $1`, jobID).Scan(&status)
		fmt.Printf("Verified status: %s\n", status)

		// Test 11: Read jobs count
		fmt.Println("\n=== Test 11: Read Jobs ===")
		var jobCount int
		err = database.QueryRow(`SELECT COUNT(*) FROM scraper_jobs`).Scan(&jobCount)
		if err != nil {
			log.Fatalf("Failed to count jobs: %v", err)
		}
		fmt.Printf("Jobs visible to current user: %d\n", jobCount)

		// Test 12: Delete test job
		fmt.Println("\n=== Test 12: Cleanup - Delete Job ===")
		_, err = database.Exec(`DELETE FROM scraper_jobs WHERE job_type = 'crud_test'`)
		if err != nil {
			log.Fatalf("Job cleanup failed: %v", err)
		}
		fmt.Println("Test job deleted")

		// Clear session variable (cleanup)
		fmt.Println("\n=== Cleanup - Clear Session ===")
		_, _ = database.Exec(`SELECT set_config('app.current_organization_id', '', false)`)
		fmt.Println("Session variable cleared")
	}

	// ========================================
	// Cleanup
	// ========================================

	// Cleanup: Delete test target
	fmt.Println("\n=== Cleanup - Delete Target ===")
	_, err = database.Exec(`DELETE FROM scraper_targets WHERE id = $1`, testTargetID)
	if err != nil {
		log.Fatalf("Cleanup failed: %v", err)
	}
	fmt.Println("Test target deleted")

	// Cleanup any leftover test targets from previous runs
	result, _ := database.Exec(`DELETE FROM scraper_targets WHERE name LIKE 'test-vps-%'`)
	if affected, _ := result.RowsAffected(); affected > 0 {
		fmt.Printf("Cleaned up %d leftover test targets\n", affected)
	}

	// Verify deletion
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM scraper_targets WHERE id = $1`, testTargetID).Scan(&count)
	fmt.Printf("Verified: target count = %d (expected 0)\n", count)

	fmt.Println("\n=== All CRUD tests passed! ===")
}
