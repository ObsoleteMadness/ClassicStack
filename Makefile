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

# Versions of the quality tools to install when absent. Kept here so a local
# `make vuln`/`make gosec` installs the same way the CI Quality job does
# (`go install ...@latest`). Pin if reproducibility becomes important.
GOVULNCHECK_PKG := golang.org/x/vuln/cmd/govulncheck@latest
GOSEC_PKG       := github.com/securego/gosec/v2/cmd/gosec@latest

# gosec scans only the packages that handle untrusted external input, matching
# the CI Quality job exactly.
GOSEC_PKGS := ./service/macip/... ./service/macgarden/... ./service/afpfs/macgarden/...

.PHONY: build build-svc test test-race test-tags lint quality vet vuln gosec fuzz clean

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

vet:
	go vet ./...

# quality runs the same static-analysis gates as the CI "Quality" job
# (vet + golangci-lint + govulncheck + gosec) from the shared script, so local
# and CI vulnerability scanning stay identical. Run `make test-race` separately
# for the race pass.
quality:
	bash scripts/ci/quality.sh

# vuln runs the same govulncheck invocation as CI, installing it on demand so
# `make vuln` works on a fresh checkout exactly as the CI step does.
vuln:
	@command -v govulncheck >/dev/null 2>&1 || go install $(GOVULNCHECK_PKG)
	govulncheck -tags all ./...

# gosec runs the same scan as CI over the untrusted-input packages, installing
# the tool on demand.
gosec:
	@command -v gosec >/dev/null 2>&1 || go install $(GOSEC_PKG)
	gosec -tags all -exclude=G115 $(GOSEC_PKGS)

fuzz:
	@for dir in protocol/ddp protocol/atp protocol/asp protocol/nbp protocol/llap; do \
	  echo "=== fuzz $$dir ==="; \
	  go test -tags all -run=^$$ -fuzz=. -fuzztime=20s ./$$dir/... || exit 1; \
	done

clean:
	rm -f classicstack classicstack.exe classicstackd classicstack-svc.exe
	rm -rf out dist
