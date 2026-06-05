TAGS ?= all

# The service/daemon wrapper is a different command per OS: a Windows service
# (classicstack-svc.exe) or a Unix daemon (classicstackd).
GOOS ?= $(shell go env GOOS)
ifeq ($(GOOS),windows)
SVC_PKG := ./cmd/classicstack-svc
SVC_BIN := classicstack-svc.exe
else
SVC_PKG := ./cmd/classicstackd
SVC_BIN := classicstackd
endif

.PHONY: build build-svc test test-race test-tags lint vuln gosec fuzz clean

build: build-svc
	go build -tags "$(TAGS)" -o classicstack ./cmd/classicstack

build-svc:
	go build -tags "$(TAGS)" -o $(SVC_BIN) $(SVC_PKG)

test:
	go test -tags "$(TAGS)" ./...

test-race:
	go test -tags "$(TAGS)" -race -count=1 ./...

test-tags:
	bash scripts/ci/test.sh

lint:
	golangci-lint run --build-tags=all --timeout=5m

vuln:
	govulncheck -tags all ./...

gosec:
	gosec -tags all ./service/macip/... ./service/macgarden/... ./service/afpfs/macgarden/...

fuzz:
	@for dir in protocol/ddp protocol/atp protocol/asp protocol/nbp protocol/llap; do \
	  echo "=== fuzz $$dir ==="; \
	  go test -tags all -run=^$$ -fuzz=. -fuzztime=20s ./$$dir/... || exit 1; \
	done

clean:
	rm -f classicstack classicstack.exe classicstackd classicstack-svc.exe
	rm -rf out dist
