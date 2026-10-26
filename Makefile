.PHONY: doctor test-tooling

doctor:
	python3 scripts/go.py version

test-tooling:
	python3 -m unittest discover -s scripts/tests -v
