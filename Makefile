TAGS ?= all

.PHONY: build test test-race test-tags lint vuln gosec fuzz clean

build:
	go build -tags "$(TAGS)" -o omnitalk ./cmd/omnitalk

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
	rm -f omnitalk omnitalk.exe
	rm -rf out dist
