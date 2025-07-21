
# Development Guide

This guide covers setting up a development environment and contributing to Plus Artifacts Server.

## Prerequisites

### Required Software

- **Go 1.21+** - [Download](https://golang.org/dl/)
- **Git** - Version control
- **Make** - Build automation
- **Docker** (optional) - For containerized development
- **curl** - For API testing

### Recommended Tools

- **VS Code** with Go extension
- **Postman** or **Insomnia** for API testing
- **golangci-lint** for code linting
- **hey** for load testing

## Environment Setup

### 1. Clone Repository

```bash
git clone https://github.com/elastic-io/plus.git
cd plus
```

### 2. Install Dependencies

```bash
# Download Go modules
go mod download

# Install development tools
make install-tools
```

### 3. Verify Setup

```bash
# Run tests
make test

# Build binary
make build

# Check code quality
make lint
```

## Development Workflow

### 1. Project Structure

```
plus/
├── cmd/
│   └── plus/              # Main application
├── internal/
│   ├── api/              # HTTP handlers
│   ├── config/           # Configuration
│   ├── metrics/          # Metrics collection
│   ├── middleware/       # HTTP middleware
│   ├── service/          # Business logic
│   └── types/            # Data structures
├── pkg/
│   ├── client/           # Go client library
│   ├── repo/             # Repository implementations
│   └── storage/          # Storage backends
├── assets/               # Embedded static files
├── docs/                 # Documentation
├── scripts/              # Build scripts
├── tests/                # Integration tests
├── testdata/             # Test fixtures
└── Makefile              # Build automation
```

### 2. Configuration

#### Development Config

Create `config.dev.yaml`:

```yaml
server:
  listen: ":8080"
  dev_mode: true
  read_timeout: "30s"
  write_timeout: "30s"

storage:
  type: "local"
  path: "./storage"

logging:
  level: "debug"
  format: "text"

security:
  cors_enabled: true
  cors_origins: ["*"]
```

#### Environment Variables

```bash
export PLUS_CONFIG=config.dev.yaml
export PLUS_LISTEN=:8080
export PLUS_STORAGE_PATH=./storage
export PLUS_LOG_LEVEL=debug
```

### 3. Running Locally

#### Basic Run

```bash
# Using config file
go run ./cmd/plus --config config.dev.yaml

# Using environment variables
PLUS_LISTEN=:8080 go run ./cmd/plus

# With debug logging
go run ./cmd/plus --config config.dev.yaml --debug
```

#### With Hot Reload

Install air for hot reloading:

```bash
go install github.com/cosmtrek/air@latest

# Run with hot reload
air
```

Create `.air.toml`:

```toml
root = "."
testdata_dir = "testdata"
tmp_dir = "tmp"

[build]
  args_bin = ["--config", "config.dev.yaml"]
  bin = "./tmp/main"
  cmd = "go build -o ./tmp/main ./cmd/plus"
  delay = 1000
  exclude_dir = ["assets", "tmp", "vendor", "testdata"]
  exclude_file = []
  exclude_regex = ["_test.go"]
  exclude_unchanged = false
  follow_symlink = false
  full_bin = ""
  include_dir = []
  include_ext = ["go", "tpl", "tmpl", "html", "yaml"]
  kill_delay = "0s"
  log = "build-errors.log"
  send_interrupt = false
  stop_on_root = false

[color]
  app = ""
  build = "yellow"
  main = "magenta"
  runner = "green"
  watcher = "cyan"

[log]
  time = false

[misc]
  clean_on_exit = false
```

## Development Tasks

### 1. Adding New Features

#### API Endpoint

1. **Define handler in `internal/api/`**:

```go
func (h *API) NewFeature(ctx *fasthttp.RequestCtx) {
    // Parse request
    // Validate input
    // Call service layer
    // Return response
}
```

2. **Add route in `SetupRouter`**:

```go
patterns["new_feature"] = regexp.MustCompile(`^/api/new-feature$`)
```

3. **Add service logic in `internal/service/`**:

```go
func (s *Service) ProcessNewFeature(ctx context.Context, input Input) (*Output, error) {
    // Business logic
    return output, nil
}
```

4. **Add tests**:

```go
func TestNewFeature(t *testing.T) {
    // Test implementation
}
```

#### Storage Backend

1. **Implement interface in `pkg/storage/`**:

```go
type NewStorage struct {
    // fields
}

func (s *NewStorage) Store(ctx context.Context, path string, reader io.Reader) error {
    // Implementation
}

// Implement other interface methods
```

2. **Register in factory**:

```go
func NewStorage(config Config) Storage {
    switch config.Type {
    case "new":
        return NewNewStorage(config)
    // other cases
    }
}
```

#### Repository Type

1. **Implement in `pkg/repo/`**:

```go
type NewRepo struct {
    storage storage.Storage
}

func (r *NewRepo) UploadPackage(ctx context.Context, repoName string, filename string, reader io.Reader) error {
    // Implementation
}

// Implement other interface methods
```

2. **Register repository type**:

```go
func init() {
    repo.Register(repo.NEW_TYPE, NewNewRepo)
}
```

### 2. Testing

#### Unit Tests

```bash
# Run all tests
make test

# Run specific package
go test ./internal/service -v

# Run with coverage
make test-coverage

# Run with race detection
go test -race ./...
```

#### Integration Tests

```bash
# Run integration tests
go test ./tests -v

# Run specific test
go test ./tests -run TestAPIEndToEnd -v
```

#### Load Testing

```bash
# Install hey
go install github.com/rakyll/hey@latest

# Test API endpoint
hey -n 1000 -c 50 http://localhost:8080/health

# Test file upload
hey -n 100 -c 10 -m POST -D test.rpm http://localhost:8080/repo/test/upload
```

#### Test Data

Create test fixtures in `testdata/`:

```
testdata/
├── packages/
│   ├── test.rpm
│   └── test.deb
├── configs/
│   └── test-config.yaml
└── responses/
    └── expected.json
```

### 3. Debugging

#### Debug Logging

```bash
# Enable debug logging
go run ./cmd/plus --config config.dev.yaml --debug

# Or set log level
PLUS_LOG_LEVEL=debug go run ./cmd/plus
```

#### Profiling

```bash
# CPU profiling
go run ./cmd/plus --config config.dev.yaml --cpuprofile=cpu.prof

# Memory profiling
go run ./cmd/plus --config config.dev.yaml --memprofile=mem.prof

# Analyze profiles
go tool pprof cpu.prof
go tool pprof mem.prof
```

#### Debugging with Delve

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug application
dlv debug ./cmd/plus -- --config config.dev.yaml

# Debug tests
dlv test ./internal/service
```

### 4. Code Quality

#### Linting

```bash
# Run all linters
make lint

# Run specific linter
golangci-lint run

# Fix auto-fixable issues
golangci-lint run --fix
```

#### Formatting

```bash
# Format code
make fmt

# Or manually
gofmt -s -w .
goimports -w .
```

#### Static Analysis

```bash
# Run go vet
go vet ./...

# Run staticcheck
staticcheck ./...
```

## Database Development

### Schema Changes

Currently, Plus uses filesystem storage. For future database support:

1. **Create migration files**:

```sql
-- migrations/001_initial.up.sql
CREATE TABLE repositories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    path VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW()
);
```

2. **Add migration logic**:

```go
func RunMigrations(db *sql.DB) error {
    // Migration logic
}
```

## Frontend Development

### Static Assets

Static files are embedded using Go's `embed` package:

```go
//go:embed static/*
var StaticFiles embed.FS
```

#### Development Mode

In development, static files are served from disk:

```yaml
server:
  dev_mode: true  # Serves from ./static/ directory
```

#### Production Mode

In production, files are served from embedded assets.

### Building Frontend

If you have a separate frontend build process:

```bash
# Build frontend assets
cd frontend
npm run build

# Copy to static directory
cp -r dist/* ../static/
```

## Docker Development

### Development Container

```dockerfile
# Dockerfile.dev
FROM golang:1.21-alpine

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o plus ./cmd/plus

EXPOSE 8080
CMD ["./plus", "--config", "config.dev.yaml"]
```

### Docker Compose for Development

```yaml
# docker-compose.dev.yml
version: '3.8'
services:
  plus:
    build:
      context: .
      dockerfile: Dockerfile.dev
    ports:
      - "8080:8080"
    volumes:
      - .:/app
      - ./storage:/app/storage
    environment:
      - PLUS_CONFIG=config.dev.yaml
    command: air  # Hot reload
```

## Performance Optimization

### Profiling

1. **Enable profiling**:

```go
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

2. **Collect profiles**:

```bash
# CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Memory profile
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine profile
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

### Benchmarking

```go
func BenchmarkUploadPackage(b *testing.B) {
    service := NewTestService()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        service.UploadPackage(context.Background(), "test", "test.rpm", bytes.NewReader(testData))
    }
}
```

Run benchmarks:

```bash
go test -bench=. ./internal/service
go test -bench=BenchmarkUploadPackage -benchmem ./internal/service
```

## Continuous Integration

### GitHub Actions

```yaml
# .github/workflows/ci.yml
name: CI
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    - uses: actions/setup-go@v3
      with:
        go-version: '1.21'
    
    - name: Run tests
      run: make test
    
    - name: Run linting
      run: make lint
    
    - name: Build
      run: make build
```

### Pre-commit Hooks

Install pre-commit hooks:

```bash
# Install pre-commit
pip install pre-commit

# Install hooks
pre-commit install
```

Create `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: local
    hooks:
      - id: go-fmt
        name: go fmt
        entry: gofmt
        language: system
        args: [-s, -w]
        files: \.go$
      
      - id: go-test
        name: go test
        entry: make test
        language: system
        pass_filenames: false
```

## Release Process

### Version Management

1. **Update version**:

```go
// internal/version/version.go
const Version = "1.1.0"
```

2. **Update changelog**:

```markdown
## [1.1.0] - 2024-02-01
### Added
- New feature X
### Fixed
- Bug Y
```

3. **Create release**:

```bash
git tag v1.1.0
git push origin v1.1.0
```

### Build Automation

```bash
# Build for multiple platforms
make build-all

# Build Docker image
make docker-build

# Create release artifacts
make release
```

## Troubleshooting

### Common Issues

1. **Port already in use**:
   ```bash
   lsof -i :8080
   kill -9 <PID>
   ```

2. **Module issues**:
   ```bash
   go mod tidy
   go clean -modcache
   go mod download
   ```

3. **Permission errors**:
   ```bash
   chmod -R 755 storage/
   ```

4. **Build errors**:
   ```bash
   go clean -cache
   go build -a ./cmd/plus
   ```

### Debug Checklist

- [ ] Check configuration file
- [ ] Verify storage permissions
- [ ] Check port availability
- [ ] Review logs for errors
- [ ] Test with minimal config
- [ ] Check Go version compatibility

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for detailed contribution guidelines.

## Getting Help

- **GitHub Issues** - Bug reports and feature requests
- **GitHub Discussions** - Questions and community support
- **Code Review** - Implementation feedback

Happy coding! 🚀