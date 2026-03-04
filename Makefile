.PHONY: build test lint clean install

BINARY := ddns
BUILD_DIR := bin
CMD := ./cmd/ddns

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) $(CMD)

install:
	go install $(CMD)

test:
	go test ./... -timeout 60s

test-verbose:
	go test ./... -v -timeout 60s

lint:
	go vet ./...
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed, skipping"

clean:
	rm -rf $(BUILD_DIR)

# Build for multiple platforms
release:
	GOOS=linux   GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-linux-amd64 $(CMD)
	GOOS=linux   GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY)-linux-arm64 $(CMD)
	GOOS=darwin  GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 $(CMD)
	GOOS=darwin  GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 $(CMD)
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe $(CMD)

# Run a local two-node test (open two terminals and run node-a, then node-b)
node-a:
	go run $(CMD) start --listen :4242 --dns-addr 127.0.0.1:5353

node-b:
	go run $(CMD) start --listen :4243 --dns-addr 127.0.0.1:5354 --seeds 127.0.0.1:4242
