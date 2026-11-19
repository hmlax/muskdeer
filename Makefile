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

doctor:
	python3 scripts/go.py version
	@for role in market strategy execution portfolio cron; do \
		python3 scripts/go.py run ./cmd/doctor -config configs/local/$$role || exit $$?; \
	done

test-tooling:
	python3 -m unittest discover -s scripts/tests -v

tools:
	python3 scripts/protoc.py
	python3 scripts/tools.py --install

contract-check:
	python3 scripts/policy.py

policy-test:
	python3 scripts/policy-negative.py

generate: contract-check
	python3 scripts/generate.py
	python3 scripts/wire.py

generate-check: contract-check
	python3 scripts/generate.py --check
	python3 scripts/wire.py --check

wire:
	python3 scripts/wire.py

wire-check:
	python3 scripts/wire.py --check

sdk-check:
	python3 scripts/check-himalaya-sdk.py
	python3 scripts/go.py test -race gitlab.com/himalaya/lukla-go/...

fmt:
	python3 scripts/format.py

fmt-check:
	python3 scripts/format.py --check

lint:
	python3 scripts/check-layout.py
	python3 scripts/lint.py

test:
	python3 scripts/go.py test ./...

test-race:
	python3 scripts/go.py test -race ./...

build:
	mkdir -p .cache/bin
	@for name in $(BUILD_COMMANDS); do \
		python3 scripts/build.py $$name ./cmd/$$name .cache/bin/$$name || exit $$?; \
	done
	@for name in $(CORE_SERVICES); do \
		python3 scripts/build.py $$name ./$$name/cmd .cache/bin/$$name || exit $$?; \
	done

verify: doctor contract-check generate-check sdk-check fmt-check lint policy-test test-tooling test test-race build

build-fixtures: build
	@for role in market strategy execution portfolio; do \
		python3 scripts/build.py module-$$role ./$$role/testservice .cache/bin/module-$$role || exit $$?; \
	done
	python3 scripts/build.py testinfra ./deer/testinfra .cache/bin/testinfra

integration: build-fixtures
	python3 scripts/integration.py

dev-up:
	python3 scripts/dev.py up

dev-down:
	python3 scripts/dev.py down

dev-status:
	python3 scripts/dev.py status

dev-test: build
	python3 scripts/dev-smoke.py

image:
	@for name in $(CORE_SERVICES); do \
		docker build --build-arg VERSION --build-arg BUILD --target runtime-$$name -t muskdeer-$$name:local . || exit $$?; \
	done

image-test: image
	python3 scripts/image-smoke.py

e2e: core-test image-test


soak: build-fixtures
	python3 scripts/soak.py

soak-smoke: build-fixtures
	python3 scripts/soak.py --smoke

compose-prepare:
	python3 scripts/compose.py prepare

compose-up: compose-prepare
	docker compose --env-file .cache/compose/compose.env build --build-arg VERSION --build-arg BUILD
	python3 scripts/compose.py bootstrap
	docker compose --env-file .cache/compose/compose.env up -d --remove-orphans --wait --wait-timeout 60

compose-down:
	docker compose --env-file .cache/compose/compose.env down

compose-status:
	docker compose --env-file .cache/compose/compose.env ps

core-test: build
	python3 scripts/core-process-test.py
