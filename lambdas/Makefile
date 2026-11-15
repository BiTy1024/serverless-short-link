# Builds one linux/arm64 "bootstrap" binary per Lambda for the
# provided.al2023 runtime. Terraform zips build/<name>/bootstrap.

FUNCTIONS := redirect links stats domains

.PHONY: build test vet fmt-check clean

build:
	@for fn in $(FUNCTIONS); do \
		echo "building $$fn"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -trimpath -buildvcs=false \
			-ldflags='-s -w' -o build/$$fn/bootstrap ./cmd/$$fn || exit 1; \
	done

test:
	go test ./...

vet:
	go vet ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

clean:
	rm -rf build
