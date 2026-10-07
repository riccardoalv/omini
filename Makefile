# Generator versions are pinned so `make generate` is reproducible.
GO_JSONSCHEMA := github.com/atombender/go-jsonschema@v0.24.1
DATAMODEL_CODEGEN := datamodel-code-generator==0.83.0

SCHEMA := schema/omini.schema.json
GO_MODEL := internal/model/model_gen.go
PY_MODEL := sdk/python/src/omini_sdk/models.py

# Python tools (ruff, pytest) come from the SDK's locked dev dependencies.
SDK := uv run --project sdk/python

.PHONY: image models generate check-generated test cover lint fmt hooks ci web run dev oui icons

## run: build the web UI and run Omini on http://localhost:8080 (scans your network)
run: web
	go run ./cmd/omini

## dev: backend (:8080) + Vite dev server with hot reload (http://localhost:5173)
dev: web/node_modules
	@echo "→ open http://localhost:5173 (UI with hot reload; the API runs on :8080)"
	@trap 'kill 0' INT TERM EXIT; \
		go run ./cmd/omini & \
		(cd web && npm run dev) & \
		wait

## web: build the web UI into web/dist (embedded into the Go binary)
web: web/node_modules
	cd web && npm run build

web/node_modules: web/package-lock.json
	cd web && npm ci
	@touch web/node_modules

## generate: regenerate Go types and Python models from the JSON Schema
generate:
	go run $(GO_JSONSCHEMA) --package model --min-sized-ints \
		--capitalization ID,IP,IPs,MAC,MACs,URL,SSID,DBM,CPU,OS,TTL \
		--tags json,yaml --output $(GO_MODEL) $(SCHEMA)
	$(SDK) --with '$(DATAMODEL_CODEGEN)' datamodel-codegen \
		--input $(SCHEMA) --input-file-type jsonschema \
		--output-model-type pydantic_v2.BaseModel --target-python-version 3.10 \
		--use-standard-collections --use-union-operator --use-annotated \
		--use-schema-description --use-field-description \
		--collapse-root-models --enum-field-as-literal all \
		--disable-timestamp --formatters ruff-format \
		--output $(PY_MODEL)
	cd sdk/python && uv run ruff format src/omini_sdk/models.py

## icons: refresh the app icon catalog and download the icon bundle embedded in release builds
icons:
	go run ./internal/appicons/gen -bundle

## oui: refresh the embedded MAC vendor database from the IEEE registry
## image: build the Docker image locally (published images come from the release workflow)
image:
	docker build --build-arg VERSION=dev -t omini:dev .

## models: refresh the device model names (Apple identifiers, Google Play devices)
models:
	go run ./internal/models/gen

oui:
	go run ./internal/oui/gen

## check-generated: fail if generated code does not match schema/ (works before committing too)
check-generated:
	@tmp=$$(mktemp -d) && cp $(GO_MODEL) $(PY_MODEL) $$tmp/ && \
	$(MAKE) -s generate >/dev/null 2>&1 && \
	if diff -q $$tmp/model_gen.go $(GO_MODEL) >/dev/null && diff -q $$tmp/models.py $(PY_MODEL) >/dev/null; then \
		echo "generated code is up to date"; \
	else \
		echo "generated code is out of date: run 'make generate' and commit the result"; exit 1; \
	fi

## test: run all test suites
test: web/node_modules
	go test ./...
	cd sdk/python && uv run pytest
	cd web && npm test

## cover: run tests with a coverage report per package
cover:
	go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
	go test -cover ./... | awk -f scripts/coverage-table.awk
	grep -v '_gen.go:' coverage.out > coverage.handwritten.out
	go tool cover -func=coverage.handwritten.out | tail -1
	cd sdk/python && uv run pytest --cov=omini_sdk --cov-report=term

## lint: run all linters (Go + Python SDK + web)
lint: web/node_modules
	golangci-lint run ./...
	cd sdk/python && uv run ruff check . && uv run ruff format --check .
	cd web && npm run lint && npm run format:check && npm run type-check

## fmt: format all code (Go + Python SDK + web)
fmt: web/node_modules
	golangci-lint fmt ./...
	cd sdk/python && uv run ruff format . && uv run ruff check --fix .
	cd web && npm run format && npm run lint:fix

## hooks: install git hooks (format, lint, conventional commit check)
hooks:
	lefthook install

## ci: run locally everything the CI workflow runs (use before pushing)
ci: check-generated
	go build ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build ./...
	go test -race ./...
	$(MAKE) lint
	cd sdk/python && uv run pytest -q
	cd web && npm test && npm run build-only
	@echo "✔ all CI checks passed locally"
