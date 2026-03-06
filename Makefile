GO             ?= go
GOFLAGS        ?=
PACKAGES        = ./...
GOLANGCI_LINT   = ./tools/bin/golangci-lint
GOLANGCI_VER    = v2.7.2
VERSION        := $(shell cat VERSION 2>/dev/null || echo v0.0.0)

.PHONY: all
all: lint test

.PHONY: test
test:
	$(GO) test $(GOFLAGS) $(PACKAGES)

.PHONY: test-v
test-v:
	$(GO) test $(GOFLAGS) -v $(PACKAGES)

.PHONY: test-race
test-race:
	$(GO) test $(GOFLAGS) -race $(PACKAGES)

.PHONY: bench
bench:
	$(GO) test $(GOFLAGS) -bench=. -benchmem $(PACKAGES)

.PHONY: lint
lint:
	$(GO) vet $(GOFLAGS) $(PACKAGES)
	@[ -x $(GOLANGCI_LINT) ] && $(GOLANGCI_LINT) run || echo "golangci-lint not found; run: make tools"

.PHONY: tools
tools:
	@mkdir -p tools/bin
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b tools/bin $(GOLANGCI_VER)

.PHONY: vet
vet:
	$(GO) vet $(GOFLAGS) $(PACKAGES)

.PHONY: format
format:
	@[ -x $(GOLANGCI_LINT) ] && $(GOLANGCI_LINT) fmt || echo "golangci-lint not found; run: make tools"

.PHONY: fmt
fmt:
	gofmt -s -w .

.PHONY: fmt-check
fmt-check:
	@test -z "$$(gofmt -s -l .)" || (echo "unformatted files:" && gofmt -s -l . && exit 1)

.PHONY: clean
clean:
	$(GO) clean $(PACKAGES)
	$(GO) clean -cache -testcache -modcache
	rm -rf tools/bin

.PHONY: tag
tag:
	git tag -a $(VERSION) -m "Release $(VERSION)"
	@echo "Tagged $(VERSION). Push with: git push origin $(VERSION)"

.PHONY: bump-major
bump-major:
	@echo $(VERSION) | sed -E 's/v([0-9]+)\..*/v'$$(echo $(VERSION) | sed -E 's/v([0-9]+)\..*/\1/' | awk '{print $$1+1}')'.0.0/' > VERSION
	@echo "$(VERSION) → $$(cat VERSION)"

.PHONY: bump-minor
bump-minor:
	@echo $(VERSION) | sed -E 's/v([0-9]+)\.([0-9]+)\..*/v\1.'$$(echo $(VERSION) | sed -E 's/v[0-9]+\.([0-9]+)\..*/\1/' | awk '{print $$1+1}')'.0/' > VERSION
	@echo "$(VERSION) → $$(cat VERSION)"

.PHONY: bump-patch
bump-patch:
	@echo $(VERSION) | sed -E 's/v([0-9]+)\.([0-9]+)\.([0-9]+).*/v\1.\2.'$$(echo $(VERSION) | sed -E 's/v[0-9]+\.[0-9]+\.([0-9]+).*/\1/' | awk '{print $$1+1}')'/' > VERSION
	@echo "$(VERSION) → $$(cat VERSION)"
