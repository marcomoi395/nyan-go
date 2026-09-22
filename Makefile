.PHONY: format check-format ci install-hooks

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
