-include .env
export

# Detect if running on Windows
ifeq ($(OS),Windows_NT)
    USE_WINDOWS := 1
else
    USE_WINDOWS :=
endif

run:
	go run cmd/service/main.go

test:
ifeq ($(USE_WINDOWS),1)
	set LOG_LEVEL=PANIC && go test ./... -v
else
	LOG_LEVEL=PANIC go test ./... --race -v
endif

test-integration:
ifeq ($(USE_WINDOWS),1)
	set BCRYPT_COST=4 && go test -tags=integration ./internal/integration/... -v
else
	BCRYPT_COST=4 go test -tags=integration ./internal/integration/... -v
endif

coverage:
ifeq ($(USE_WINDOWS),1)
	set LOG_LEVEL=PANIC && go test ./... --coverprofile=coverage.out
	go tool cover -html=coverage.out
else
	LOG_LEVEL=PANIC go test ./... --coverprofile=coverage.out
	go tool cover -html=coverage.out
endif

sonarqube-run: coverage
	sonar-scanner \
	  -Dsonar.projectKey=auto-repair-shop \
	  -Dsonar.sources=. \
	  -Dsonar.host.url=http://localhost:9000 \
	  -Dsonar.token=${SONARQUBE_PROJECT_TOKEN}

docker-up:
	docker-compose up --build

docker-down:
	docker-compose down

migrate-install:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

migrate-up:
	@type migrate >/dev/null 2>&1 || { $(MAKE) migrate-install; }
	migrate -path migrations -database "postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/autorepairshop?sslmode=disable" up

migrate-down:
	@type migrate >/dev/null 2>&1 || { $(MAKE) migrate-install; }
	migrate -path migrations -database "postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/autorepairshop?sslmode=disable" down

migrate-status:
	@type migrate >/dev/null 2>&1 || { $(MAKE) migrate-install; }
	migrate -path migrations -database "postgresql://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/autorepairshop?sslmode=disable" version

mockgen:
	go generate ./...

# =====================================================================
# Kubernetes / Infra (k8s/) — 100% Docker-only
#   The host needs ONLY Docker. These targets run the real work inside the
#   toolbox container (kind/kubectl/terraform/aws) via the mounted Docker
#   socket. For anything ad hoc (kubectl, logs, terraform, aws) use k8s-shell.
#   See README "Infrastructure (k8s/)".
# =====================================================================

K8S_TOOLBOX := ./k8s/toolbox/run.sh

.PHONY: k8s-up k8s-down k8s-shell

k8s-up: ## Bring up the full local environment (Kind + app + HPA)
	@$(K8S_TOOLBOX) up

k8s-down: ## Tear down the local environment (delete the Kind cluster)
	@$(K8S_TOOLBOX) down

k8s-shell: ## Shell into the toolbox for anything else (kubectl/kind/terraform/aws)
	@$(K8S_TOOLBOX) shell
