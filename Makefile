TAGS ?= all

# The service/daemon wrapper is a different command per OS: a Windows service
# (classicstack-svc.exe) or a Unix daemon (classicstackd).
GOOS ?= $(shell go env GOOS)
ifeq ($(GOOS),windows)
SVC_PKG := ./cmd/classicstack-svc
SVC_BIN := classicstack-svc.exe
MOUNT_BIN := csmount.exe
MOUNT_TAGS := $(TAGS)
else ifeq ($(filter $(GOOS),darwin linux),$(GOOS))
SVC_PKG := ./cmd/classicstackd
SVC_BIN := classicstackd
MOUNT_BIN := csmount
# fuse tag pulls in cgofuse (macFUSE / libfuse). Requires cgo + FUSE headers.
MOUNT_TAGS := $(TAGS) fuse
else
SVC_PKG := ./cmd/classicstackd
SVC_BIN := classicstackd
MOUNT_BIN :=
MOUNT_TAGS := $(TAGS)
endif

# Versions of the quality tools to install when absent. Kept here so a local
# `make vuln`/`make gosec` installs the same way the CI Quality job does
# (`go install ...@latest`). Pin if reproducibility becomes important.
GOVULNCHECK_PKG := golang.org/x/vuln/cmd/govulncheck@latest
GOSEC_PKG       := github.com/securego/gosec/v2/cmd/gosec@latest

# gosec scans only the packages that handle untrusted external input, matching
# the CI Quality job exactly.
GOSEC_PKGS := ./service/macip/... ./service/macgarden/... ./service/afpfs/macgarden/...

.PHONY: build build-svc build-mount spa test test-race test-tags lint quality vet vuln gosec fuzz clean \
        harness archtest tinygo-gate

# Vite SPA (Finder + admin). Required for TAGS that embed webui (all, webui).
# Uses third_party/classicstack-web, sibling ../ClassicStack-web, or WEB_REF clone.
spa:
	bash scripts/ci/spa.sh

ifneq ($(filter all webui,$(TAGS)),)
build: spa
endif

# On macOS, embed Info.plist into the Mach-O so Local Network privacy (TN3179) can
# show a usage string. Sending/receiving LToUDP multicast is a local-network
# operation; a CLI from Terminal is auto-allowed, but a binary launched from
# Finder/an IDE needs this (and the user's Allow). Requires the external linker
# (cgo), which the default pcap tag already enables.
DARWIN_INFOPLIST := $(CURDIR)/packaging/darwin/Info.plist
ifeq ($(GOOS),darwin)
LDFLAGS += -linkmode=external -extldflags=-Wl,-sectcreate,__TEXT,__info_plist,$(DARWIN_INFOPLIST)
endif

build: build-svc build-mount
	go build -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o classicstack ./cmd/classicstack

build-svc:
	go build -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o $(SVC_BIN) $(SVC_PKG)

# build-mount builds the host mount client (WinFsp on Windows, FUSE on Darwin/Linux).
build-mount:
ifneq ($(MOUNT_BIN),)
	go build -tags "$(MOUNT_TAGS)" -o $(MOUNT_BIN) ./cmd/csmount
endif

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

# --- Phase 1 (refactor) harness gates -------------------------------------
# The greenfield core/adapter/compose rings have their own guardrails, kept
# separate from the legacy targets above. See .refactor/01-PHASE-harness.md.

# harness runs the same gates as the Refactor Harness CI job: build (default +
# tags=all), vet of the new rings, the import-graph archtest, and the core/
# unit tests (which grow as B*/C*/E* land).
harness:
	bash scripts/ci/harness.sh

# archtest is the import-graph dependency rule (§1) in isolation, for a quick
# local check after touching a core/ import.
archtest:
	go test -count=1 ./core/internal/archtest/...

# tinygo-gate runs the TinyGo amd64 build gates (linux + windows). Requires
# tinygo on PATH; CI installs it. This is how the no-reflection /
# no-forbidden-import discipline is verified without ESP32 hardware.
tinygo-gate:
	bash scripts/ci/tinygo-gate.sh

# --- Hardware Build Targets ---
build-wt32eth01:
	bash scripts/build_wt32eth01.sh

build-pico:
	bash scripts/build_pico.sh pico

build-picow:
	bash scripts/build_pico.sh picow

build-pico2:
	bash scripts/build_pico.sh pico2

build-pico2w:
	bash scripts/build_pico.sh pico2w

clean:
	rm -f classicstack classicstack.exe classicstackd classicstack-svc.exe csmount csmount.exe cs-tinygo.exe
	rm -rf out dist bin/*.bin bin/*.uf2

