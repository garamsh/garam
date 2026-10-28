# cowork/engine — the package that bundles cowork's Yjs engine into the
# one script cowork's Go adapter embeds (ADR-0127). It holds only the
# bundler and Yjs, so it has no gate of its own: what it builds is
# checked by `make -C cowork check-gen`, which runs `build` here.

.PHONY: install build

PNPM := pnpm --dir .

install:
	$(PNPM) install --frozen-lockfile

build: install
	$(PNPM) build
