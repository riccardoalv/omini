# Generator versions are pinned so `make generate` is reproducible.
GO_JSONSCHEMA := github.com/atombender/go-jsonschema@v0.24.1
DATAMODEL_CODEGEN := datamodel-code-generator==0.83.0

SCHEMA := schema/omini.schema.json
GO_MODEL := internal/model/model_gen.go
PY_MODEL := sdk/python/src/omini_sdk/models.py

.PHONY: generate check-generated test lint fmt hooks

## generate: regenerate Go types and Python models from the JSON Schema
generate:
	go run $(GO_JSONSCHEMA) --package model --min-sized-ints \
		--capitalization ID,IP,IPs,MAC,MACs,URL,SSID,DBM,CPU,OS \
		--tags json,yaml --output $(GO_MODEL) $(SCHEMA)
	uvx --from '$(DATAMODEL_CODEGEN)' datamodel-codegen \
		--input $(SCHEMA) --input-file-type jsonschema \
		--output-model-type pydantic_v2.BaseModel --target-python-version 3.10 \
		--use-standard-collections --use-union-operator --use-annotated \
		--use-schema-description --use-field-description \
		--collapse-root-models --enum-field-as-literal all \
		--disable-timestamp --formatters black isort \
		--output $(PY_MODEL)

## check-generated: fail if generated code is out of date (used in CI)
check-generated: generate
	git diff --exit-code -- $(GO_MODEL) $(PY_MODEL)

## test: run all test suites
test:
	go test ./...
	cd sdk/python && uv run --extra test pytest

## lint: run all linters (Go + Python SDK)
lint:
	golangci-lint run ./...
	uvx ruff check sdk/python
	uvx ruff format --check sdk/python

## fmt: format all code (Go + Python SDK)
fmt:
	golangci-lint fmt ./...
	uvx ruff format sdk/python
	uvx ruff check --fix sdk/python

## hooks: install git hooks (format, lint, conventional commit check)
hooks:
	lefthook install
