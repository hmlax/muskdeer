.PHONY: build build_only run api models wire wire-check
build build_only:
	$(MAKE) -C .. admin-backend-build
run:
	cd .. && python3 scripts/go.py run ./operator/cmd/server
api models:
	$(MAKE) -C .. admin-generate
wire:
	$(MAKE) -C .. admin-wire
wire-check:
	$(MAKE) -C .. admin-wire-check
