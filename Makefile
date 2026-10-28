.PHONY: doctor test-tooling tools contract-check policy-test generate generate-check hz-check fmt fmt-check lint test test-race build verify integration dev-up dev-down dev-status dev-test image image-test e2e web-install web-build web-check web-generated-check admin-browser

doctor:
	python3 scripts/go.py version
	python3 scripts/go.py run ./cmd/doctor -config configs/examples/service.json
	python3 scripts/go.py run ./cmd/doctor -config configs/examples/api.json
	@for role in api market strategy execution portfolio cron; do \
		python3 scripts/go.py run ./cmd/doctor -config configs/local/$$role.json || exit $$?; \
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

generate-check: contract-check
	python3 scripts/generate.py --check

hz-check:
	python3 scripts/verify-hz.py

fmt:
	python3 scripts/format.py

fmt-check:
	python3 scripts/format.py --check

lint:
	python3 scripts/lint.py

test:
	python3 scripts/go.py test ./...

test-race:
	python3 scripts/go.py test -race ./...

build:
	python3 scripts/go.py build ./...
	mkdir -p .cache/bin
	python3 scripts/go.py build -o .cache/bin/muskdeer ./cmd/muskdeer
	python3 scripts/go.py build -o .cache/bin/migrate ./cmd/migrate
	python3 scripts/go.py build -o .cache/bin/admin ./cmd/admin

verify: doctor contract-check generate-check hz-check fmt-check lint policy-test test-tooling test test-race build

integration: build
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
	docker build --target runtime -t muskdeer:local .

image-test: image
	python3 scripts/image-smoke.py

web-install:
	python3 scripts/web.py install

web-build:
	python3 scripts/web.py build

web-check:
	python3 scripts/web.py check

web-generated-check: web-build
	git diff --exit-code -- web/admin/dist

admin-browser: build
	python3 scripts/web.py browser-install
	python3 scripts/admin-browser.py

e2e: integration image-test admin-browser

.PHONY: soak soak-smoke
soak: build
	python3 scripts/soak.py

soak-smoke: build
	python3 scripts/soak.py --smoke
