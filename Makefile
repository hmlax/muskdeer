.PHONY: doctor test-tooling tools generate generate-check hz-check fmt fmt-check lint test test-race build verify integration

doctor:
	python3 scripts/go.py version
	python3 scripts/go.py run ./cmd/doctor -config configs/examples/service.json
	python3 scripts/go.py run ./cmd/doctor -config configs/examples/api.json

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

verify: doctor generate-check hz-check fmt-check lint test-tooling test test-race build

integration:
	python3 scripts/integration.py
