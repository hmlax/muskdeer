.PHONY: doctor test-tooling tools generate generate-check hz-check fmt fmt-check lint test test-race build verify integration

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

generate:
	python3 scripts/generate.py

generate-check:
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

verify: doctor generate-check hz-check fmt-check lint test-tooling test test-race build

integration: build
	python3 scripts/integration.py
