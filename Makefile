.PHONY: format check-format ci install-hooks tag tag-dry-run

format:
	git ls-files -z '*.go' | xargs -0 -r gofmt -w

check-format:
	@test -z "$$(git ls-files -z '*.go' | xargs -0 -r gofmt -l)" || (git ls-files -z '*.go' | xargs -0 -r gofmt -l; exit 1)

ci: check-format
	go vet ./...
	go test ./...
	go build ./...

install-hooks:
	git config core.hooksPath .githooks

tag:
	@set -eu; \
	# ponytail: stable MAJOR.MINOR.PATCH only; extend this rule for prereleases. \
	latest=$$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | head -n1 || true); \
	if [ -z "$$latest" ]; then next=v0.1.0; else IFS=.; set -- $${latest#v}; next="v$$1.$$2.$$(( $$3 + 1 ))"; fi; \
	if [ "$${TAG_DRY_RUN:-0}" = 1 ]; then printf 'Next tag: %s\n' "$$next"; else git tag -a "$$next" -m "Release $$next"; printf 'Created tag: %s\n' "$$next"; fi

tag-dry-run:
	@$(MAKE) --no-print-directory tag TAG_DRY_RUN=1
