.PHONY: all build run test proto clean deploy

# Build settings
BINARY_NAME=lb-scrape

# Build the application
build:
	go build -o $(BINARY_NAME) .

# Run the application
run: build
	./$(BINARY_NAME)

# Run tests
test:
	go test ./...

# Run tests with verbose output
test-v:
	go test -v ./...

# Generate protobuf code
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/scraper.proto
	mv proto/*.pb.go pkg/pb/

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	rm -f *.exe

# Download dependencies
deps:
	go mod tidy

# Run go vet
vet:
	go vet ./...

# Build and test
check: vet test build

# Full rebuild
rebuild: clean deps build

# Docker settings
IMAGE_NAME=asia-northeast1-docker.pkg.dev/cloudsql-sv/lb-scrape/lb-scrape
IMAGE_TAG=latest

# Cloud Run settings
PROJECT_ID=cloudsql-sv
REGION=asia-northeast1
CLOUDSQL_INSTANCE=postgres-prod
DB_NAME=myapp
DB_USER="747065218280-compute@developer"
SERVICE_ACCOUNT=747065218280-compute@developer.gserviceaccount.com

# gcloud path (Windows)
GCLOUD="/c/Users/mtama/AppData/Local/Google/Cloud SDK/google-cloud-sdk/bin/gcloud.cmd"

# Authenticate Docker to Artifact Registry
docker-auth:
	@TOKEN=$$($(GCLOUD) auth print-access-token) && docker login -u oauth2accesstoken -p $$TOKEN https://asia-northeast1-docker.pkg.dev

# Build Docker image locally
docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

# Push Docker image to Artifact Registry
docker-push:
	docker push $(IMAGE_NAME):$(IMAGE_TAG)

# Deploy to Cloud Run
cloud-run-deploy:
	$(GCLOUD) run deploy scraper-lb \
		--image=$(IMAGE_NAME):$(IMAGE_TAG) \
		--region=$(REGION) \
		--platform=managed \
		--allow-unauthenticated \
		--add-cloudsql-instances=$(PROJECT_ID):$(REGION):$(CLOUDSQL_INSTANCE) \
		--set-env-vars="CLOUDSQL_ENABLED=true,CLOUDSQL_INSTANCE=$(PROJECT_ID):$(REGION):$(CLOUDSQL_INSTANCE),DB_USER=$(DB_USER),DB_NAME=$(DB_NAME)" \
		--use-http2 \
		--project=$(PROJECT_ID)

# Local build and deploy (no Cloud Build charges)
deploy-local: docker-build docker-auth docker-push cloud-run-deploy

# Force deploy with timestamp tag (always creates new revision)
deploy-force:
	$(eval TIMESTAMP := $(shell date +%Y%m%d%H%M%S))
	docker build -t $(IMAGE_NAME):$(TIMESTAMP) .
	@TOKEN=$$($(GCLOUD) auth print-access-token) && docker login -u oauth2accesstoken -p $$TOKEN https://asia-northeast1-docker.pkg.dev
	docker push $(IMAGE_NAME):$(TIMESTAMP)
	$(GCLOUD) run deploy scraper-lb \
		--image=$(IMAGE_NAME):$(TIMESTAMP) \
		--region=$(REGION) \
		--platform=managed \
		--allow-unauthenticated \
		--add-cloudsql-instances=$(PROJECT_ID):$(REGION):$(CLOUDSQL_INSTANCE) \
		--set-env-vars="CLOUDSQL_ENABLED=true,CLOUDSQL_INSTANCE=$(PROJECT_ID):$(REGION):$(CLOUDSQL_INSTANCE),DB_USER=$(DB_USER),DB_NAME=$(DB_NAME)" \
		--use-http2 \
		--project=$(PROJECT_ID)

# Source deploy (uses Cloud Build)
deploy-source:
	$(GCLOUD) run deploy scraper-lb \
		--source . \
		--region=$(REGION) \
		--platform=managed \
		--allow-unauthenticated \
		--add-cloudsql-instances=$(PROJECT_ID):$(REGION):$(CLOUDSQL_INSTANCE) \
		--set-env-vars="CLOUDSQL_ENABLED=true,CLOUDSQL_INSTANCE=$(PROJECT_ID):$(REGION):$(CLOUDSQL_INSTANCE),DB_USER=$(DB_USER),DB_NAME=$(DB_NAME)" \
		--use-http2 \
		--project=$(PROJECT_ID)
