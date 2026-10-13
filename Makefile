# Go 1.24+, docker-agent v1.150.0+ (as `docker-agent` or `docker agent`).
DOCKER_AGENT ?= $(shell command -v docker-agent 2>/dev/null)
export DOCKER_AGENT TELEMETRY_ENABLED := false

.PHONY: all lint test integration stub evals clean

all: lint test integration

lint: ## gofmt + go vet
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go vet -tags integration ./...

test: ## unit tests for the stub model
	go test -race -cover ./...

integration: ## real docker-agent runtime + stub model, both configs (SPEC R1-R9, F1)
	go test -tags integration -count=1 -v ./integration/...

stub: ## run the stub model on 127.0.0.1:8089
	go run ./cmd/stubmodel

evals/working_dirs/sample: target .agentsignore
	rm -rf $@ && mkdir -p $@/reports
	cp -R target .agentsignore $@/

evals: evals/working_dirs/sample ## quality evals against real models (SPEC E1-E3); needs API keys + Docker
	docker agent eval crew.yaml evals --judge-model anthropic/claude-sonnet-4-5 \
	  -e ANTHROPIC_API_KEY

clean:
	rm -rf evals/working_dirs evals/results reports/gate-report.md
