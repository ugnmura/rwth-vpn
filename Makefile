.PHONY: build test check release publish

VERSION ?= dev

build:
	@mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/rwth-vpn ./cmd/rwth-vpn

test:
	go test ./...

check:
	go vet ./...
	sh -n install.sh
	bash -n internal/runtime/rwth-vpn.sh
	@for script in scripts/build-releases.sh scripts/publish-release.sh tests/secret-service.sh lab/install-repo.sh; do sh -n "$$script"; done

release:
	sh scripts/build-releases.sh $(VERSION)

publish:
	sh scripts/publish-release.sh $(VERSION)
