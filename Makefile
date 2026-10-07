# Generator versions are pinned so `make generate` is reproducible.
GO_JSONSCHEMA := github.com/atombender/go-jsonschema@v0.24.1
DATAMODEL_CODEGEN := datamodel-code-generator==0.83.0

SCHEMA := schema/omini.schema.json
GO_MODEL := internal/model/model_gen.go
PY_MODEL := sdk/python/src/omini_sdk/models.py

# Python tools (ruff, pytest) come from the SDK's locked dev dependencies.
SDK := uv run --project sdk/python

.PHONY: generate check-generated test cover lint fmt hooks ci

## generate: regenerate Go types and Python models from the JSON Schema
generate:
	go run $(GO_JSONSCHEMA) --package model --min-sized-ints \
		--capitalization ID,IP,IPs,MAC,MACs,URL,SSID,DBM,CPU,OS \
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

## check-generated: fail if generated code is out of date (used in CI)
check-generated: generate
	git diff --exit-code -- $(GO_MODEL) $(PY_MODEL)

## test: run all test suites
test:
	go test ./...
	cd sdk/python && uv run pytest

## cover: run tests with a coverage report per package
cover:
	go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
	go test -cover ./... | awk -f scripts/coverage-table.awk
	grep -v '_gen.go:' coverage.out > coverage.handwritten.out
	go tool cover -func=coverage.handwritten.out | tail -1
	cd sdk/python && uv run pytest --cov=omini_sdk --cov-report=term

## lint: run all linters (Go + Python SDK)
lint:
	golangci-lint run ./...
	cd sdk/python && uv run ruff check . && uv run ruff format --check .

## fmt: format all code (Go + Python SDK)
fmt:
	golangci-lint fmt ./...
	cd sdk/python && uv run ruff format . && uv run ruff check --fix .

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
	@echo "✔ all CI checks passed locally"
