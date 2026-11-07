.PHONY: doctor test-tooling tools contract-check policy-test generate generate-check wire wire-check sdk-check fmt fmt-check lint test test-race build verify build-fixtures integration dev-up dev-down dev-status dev-test image image-test e2e soak soak-smoke compose-prepare compose-up compose-down compose-status core-test

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
	python3 scripts/go.py build -o .cache/bin/migrate ./cmd/migrate
	python3 scripts/go.py build -o .cache/bin/coreinit ./cmd/coreinit
	python3 scripts/go.py build -o .cache/bin/policyinit ./cmd/policyinit
	python3 scripts/go.py build -o .cache/bin/readycheck ./cmd/readycheck
	python3 scripts/go.py build -o .cache/bin/venueinit ./cmd/venueinit
	python3 scripts/go.py build -o .cache/bin/venuebaseline ./cmd/venuebaseline
	python3 scripts/go.py build -o .cache/bin/instancemigrate ./cmd/instancemigrate
	python3 scripts/go.py build -o .cache/bin/instanceupgrade ./cmd/instanceupgrade
	@for name in market strategy execution portfolio cron; do \
		python3 scripts/go.py build -o .cache/bin/$$name ./$$name/cmd || exit $$?; \
	done

verify: doctor contract-check generate-check sdk-check fmt-check lint policy-test test-tooling test test-race build

build-fixtures: build
	@for role in market strategy execution portfolio; do \
		python3 scripts/go.py build -o .cache/bin/module-$$role ./$$role/testservice || exit $$?; \
	done

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
	@for name in market strategy execution portfolio cron; do \
		docker build --target runtime-$$name -t muskdeer-$$name:local . || exit $$?; \
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
	docker compose --env-file .cache/compose/compose.env build
	python3 scripts/compose.py bootstrap
	docker compose --env-file .cache/compose/compose.env up -d --remove-orphans --wait --wait-timeout 60

compose-down:
	docker compose --env-file .cache/compose/compose.env down

compose-status:
	docker compose --env-file .cache/compose/compose.env ps

core-test: build
	python3 scripts/core-process-test.py
