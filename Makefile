MODULE := $(shell go list -m)
BIN_DIR := bin
BINARY := $(BIN_DIR)/goapplicationframework

all: clean build coverage

.PHONY: all build clean coverage goapplicationframework

build: $(BINARY)

$(BINARY):
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -o $@ -ldflags "-X $(MODULE)/config.BuildTimestamp=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)"

# Keep original target name as an alias
goapplicationframework: $(BINARY)

clean:
	rm -rf $(BIN_DIR)
	rm -f coverage.out coverage.html

coverage:
	go test -cover -coverpkg=./... -coverprofile=coverage.out ./... || true
	go tool cover -html=coverage.out -o coverage.html
