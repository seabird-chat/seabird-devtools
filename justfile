_default:
    @just --list

# Build the binary into tmp/
build:
    mkdir -p tmp
    CGO_ENABLED=0 go build -o tmp/seabird-dev ./cmd/seabird-dev

# Remove build artifacts
clean:
    rm -rf tmp/

# Run all tests (e.g. just test -run TestFormatEvent)
test *args="":
    CGO_ENABLED=0 go test ./... {{ args }}

# Run all tests with the race detector, which needs cgo
test-race *args="":
    go test -race ./... {{ args }}

# Run go vet
vet:
    go vet ./...

# Run the checks to do before committing
check: vet test-race

# Format all code (pass --fail-on-change to check only)
fmt *args="":
    treefmt {{ args }}

# Run from source (e.g. just run plugin --token sbt_...)
run *args="":
    go run ./cmd/seabird-dev {{ args }}
