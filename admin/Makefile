.PHONY: build build_only run api hz openapi ts
build build_only:
	$(MAKE) -C .. admin-backend-build
run:
	cd .. && python3 scripts/go.py run ./admin
api hz:
	$(MAKE) -C .. admin-generate
openapi:
	$(MAKE) -C .. admin-openapi
ts:
	$(MAKE) -C .. admin-ts
