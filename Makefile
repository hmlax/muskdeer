PYTHON ?= python3

.PHONY: doctor test-tooling tools contract-check policy-test generate generate-check wire wire-check sdk-check fmt fmt-check lint test test-race build verify build-fixtures integration dev-up dev-down dev-status dev-test image image-test e2e soak soak-smoke compose-prepare compose-up compose-down compose-status core-test

# Freeze one version/build record for every binary in this Make invocation.
# scripts/build.py injects -X main.Name, -X main.Version and -X main.Build.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION := $(VERSION)
BUILD ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)@$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD := $(BUILD)
export NAME VERSION BUILD
CORE_SERVICES := market strategy execution portfolio cron
BUILD_COMMANDS := migrate coreinit policyinit readycheck venueinit venuebaseline instancemigrate instanceupgrade doctor marketprobe contractcheck
PNPM := npm exec --yes --package=pnpm@12.16.1 -- pnpm --store-dir="$(CURDIR)/.cache/pnpm-store"
export npm_config_cache := $(CURDIR)/.cache/npm

doctor:
	$(PYTHON) scripts/go.py version
	@for role in market strategy execution portfolio cron; do \
		$(PYTHON) scripts/go.py run ./cmd/doctor -config configs/local/$$role || exit $$?; \
	done

test-tooling:
	$(PYTHON) -m unittest discover -s scripts/tests -v

tools:
	$(PYTHON) scripts/protoc.py
	$(PYTHON) scripts/tools.py --install
	$(PYTHON) scripts/admin.py tools

contract-check:
	$(PYTHON) scripts/policy.py

policy-test:
	$(PYTHON) scripts/policy-negative.py

generate: contract-check
	$(PYTHON) scripts/generate.py
	$(PYTHON) scripts/wire.py
	$(PYTHON) scripts/admin.py generate

generate-check: contract-check
	$(PYTHON) scripts/generate.py --check
	$(PYTHON) scripts/wire.py --check
	$(PYTHON) scripts/admin.py check

wire:
	$(PYTHON) scripts/wire.py

wire-check:
	$(PYTHON) scripts/wire.py --check

sdk-check:
	$(PYTHON) scripts/check-himalaya-sdk.py
	$(PYTHON) scripts/go.py test -race gitlab.com/himalaya/lukla-go/...

fmt:
	$(PYTHON) scripts/format.py

fmt-check:
	$(PYTHON) scripts/format.py --check

lint:
	$(PYTHON) scripts/check-layout.py
	$(PYTHON) scripts/lint.py

test:
	$(PYTHON) scripts/go.py test ./...

test-race:
	$(PYTHON) scripts/go.py test -race ./...

build:
	mkdir -p .cache/bin
	@for name in $(BUILD_COMMANDS); do \
		$(PYTHON) scripts/build.py $$name ./cmd/$$name .cache/bin/$$name || exit $$?; \
	done
	@for name in $(CORE_SERVICES); do \
		$(PYTHON) scripts/build.py $$name ./$$name/cmd .cache/bin/$$name || exit $$?; \
	done
	$(MAKE) admin-backend-build

verify: doctor contract-check generate-check sdk-check fmt-check lint policy-test test-tooling test test-race build admin-frontend-check

build-fixtures: build
	@for role in market strategy execution portfolio; do \
		$(PYTHON) scripts/build.py module-$$role ./$$role/testservice .cache/bin/module-$$role || exit $$?; \
	done
	$(PYTHON) scripts/build.py testinfra ./deer/testinfra .cache/bin/testinfra

integration: build-fixtures
	$(PYTHON) scripts/integration.py

dev-up:
	$(PYTHON) scripts/dev.py up

dev-down:
	$(PYTHON) scripts/dev.py down

dev-status:
	$(PYTHON) scripts/dev.py status

dev-test: build
	$(PYTHON) scripts/dev-smoke.py

image:
	@for name in $(CORE_SERVICES); do \
		docker build --build-arg VERSION --build-arg BUILD --target runtime-$$name -t muskdeer-$$name:local . || exit $$?; \
	done

image-test: image
	$(PYTHON) scripts/image-smoke.py

e2e: core-test image-test


soak: build-fixtures
	$(PYTHON) scripts/soak.py

soak-smoke: build-fixtures
	$(PYTHON) scripts/soak.py --smoke

compose-prepare:
	$(PYTHON) scripts/compose.py prepare

compose-up: compose-prepare
	docker compose --env-file .cache/compose/compose.env build --build-arg VERSION --build-arg BUILD
	$(PYTHON) scripts/compose.py bootstrap
	docker compose --env-file .cache/compose/compose.env up -d --wait --wait-timeout 60

compose-down:
	docker compose --env-file .cache/compose/compose.env down

compose-status:
	docker compose --env-file .cache/compose/compose.env ps

core-test: build
	$(PYTHON) scripts/core-process-test.py

# Administration shares this module; only the framework database remains separate.
.PHONY: admin-tools admin-deps admin-generate admin-generate-check admin-wire admin-wire-check admin-openapi admin-ts admin-backend-build admin-frontend-build admin-build admin-test admin-check admin-dev-platform admin-dev-merchant build-all
admin-tools:
	$(PYTHON) scripts/admin.py tools
admin-deps:
	cd adminfrontend && $(PNPM) install --frozen-lockfile
admin-generate:
	$(PYTHON) scripts/admin.py generate
admin-generate-check:
	$(PYTHON) scripts/admin.py check
admin-wire:
	$(PYTHON) scripts/admin.py wire
admin-wire-check:
	$(PYTHON) scripts/admin.py wire-check
admin-openapi:
	$(PYTHON) scripts/admin/tools/codegen.py openapi
	$(PYTHON) scripts/go.py run ./scripts/admin/tools/openapigen deer/management/apidocs/openapi.yaml
admin-ts:
	$(PYTHON) scripts/admin/tools/codegen.py ts
admin-backend-build:
	$(PYTHON) scripts/build.py admin ./admin .cache/bin/admin
	$(PYTHON) scripts/build.py operator ./operator/cmd/server .cache/bin/operator
	$(PYTHON) scripts/build.py muskdeer-admin-bootstrap ./operator/cmd/muskdeer-bootstrap .cache/bin/muskdeer-admin-bootstrap
	$(PYTHON) scripts/build.py muskdeer-admin-migrate ./operator/cmd/muskdeer-migrate .cache/bin/muskdeer-admin-migrate
admin-frontend-build:
	cd adminfrontend && $(PNPM) build
admin-build: admin-backend-build admin-frontend-build
admin-test:
	$(PYTHON) scripts/go.py test ./admin/... ./operator/... ./deer/management/... ./scripts/admin/tools/...
	$(PYTHON) -m unittest discover -s scripts/admin/test -p 'test_*.py' -v
	cd adminfrontend && $(PNPM) test:tenant-routes
admin-check: admin-test admin-wire-check admin-generate-check
	cd adminfrontend && $(PNPM) typecheck
admin-dev-platform:
	cd adminfrontend && $(PNPM) dev:platform
admin-dev-merchant:
	cd adminfrontend && $(PNPM) dev:merchant
build-all: build admin-frontend-build

.PHONY: admin-frontend-check
admin-frontend-check:
	cd adminfrontend && $(PNPM) typecheck
	cd adminfrontend && $(PNPM) test:tenant-routes
