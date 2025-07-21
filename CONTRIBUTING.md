# Contributing to Plus Artifacts Server

Thank you for your interest in contributing to Plus! This document provides guidelines and information for contributors.

## 🤝 How to Contribute

### Reporting Issues

Before creating an issue, please:

1. **Search existing issues** to avoid duplicates
2. **Use the issue templates** when available
3. **Provide detailed information** including:
   - Plus version
   - Operating system
   - Go version (if building from source)
   - Steps to reproduce
   - Expected vs actual behavior
   - Relevant logs or error messages

### Suggesting Features

We welcome feature suggestions! Please:

1. **Check the roadmap** in README.md first
2. **Open a discussion** before implementing large features
3. **Describe the use case** and why it's valuable
4. **Consider backward compatibility**

### Code Contributions

#### Getting Started

1. **Fork the repository**
   ```bash
   git clone https://github.com/elastic-io/plus.git
   cd plus
   ```

2. **Set up development environment**
   ```bash
   # Install Go 1.21 or later
   go version

   # Install dependencies
   go mod download

   # Run tests
   make test
   ```

3. **Create a feature branch**
   ```bash
   git checkout -b feature/your-feature-name
   ```

#### Development Workflow

1. **Make your changes**
   - Follow the coding standards below
   - Add tests for new functionality
   - Update documentation as needed

2. **Test your changes**
   ```bash
   # Run all tests
   make test

   # Run with coverage
   make test-coverage

   # Run linting
   make lint

   # Test locally
   go run ./cmd/plus --config config.dev.yaml
   ```

3. **Commit your changes**
   ```bash
   git add .
   git commit -m "feat: add new feature description"
   ```

4. **Push and create PR**
   ```bash
   git push origin feature/your-feature-name
   ```

## 📝 Coding Standards

### Go Code Style

We follow standard Go conventions:

- **gofmt** for formatting
- **golint** for linting
- **go vet** for static analysis
- **Effective Go** guidelines

#### Naming Conventions

```go
// Good
type PackageManager struct {
    repoService *service.RepoService
}

func (pm *PackageManager) UploadPackage(ctx context.Context, name string) error {
    // implementation
}

// Bad
type pkgMgr struct {
    repo_svc *service.RepoService
}

func (p *pkgMgr) upload_pkg(ctx context.Context, n string) error {
    // implementation
}
```

#### Error Handling

```go
// Good - wrap errors with context
func (s *Service) ProcessPackage(filename string) error {
    data, err := os.ReadFile(filename)
    if err != nil {
        return fmt.Errorf("failed to read package file %s: %w", filename, err)
    }
    // process data
    return nil
}

// Bad - ignore or lose error context
func (s *Service) ProcessPackage(filename string) error {
    data, _ := os.ReadFile(filename) // Don't ignore errors
    // process data
    return nil
}
```

#### Logging

```go
// Good - structured logging
log.Printf("🔍 Processing package: repo=%s, file=%s", repoName, filename)
log.Printf("✅ Package uploaded successfully: %s", filename)
log.Printf("❌ Upload failed: repo=%s, error=%v", repoName, err)

// Use consistent emoji prefixes:
// 🔍 for debug/info
// ✅ for success
// ❌ for errors
// 🔄 for processing
// 📁 for file operations
```

### API Design

#### REST Endpoints

```go
// Good - RESTful design
GET    /repos                    // List repositories
POST   /repos                    // Create repository
GET    /repo/{name}              // Get repository info
DELETE /repo/{name}              // Delete repository
POST   /repo/{name}/upload       // Upload package
POST   /repo/{name}/refresh      // Refresh metadata
```

#### Response Format

```go
// Consistent response structure
type Response struct {
    Status  Status      `json:"status"`
    Data    interface{} `json:"data,omitempty"`
}

type Status struct {
    Status  string `json:"status"`  // "success" or "error"
    Message string `json:"message"`
    Code    int    `json:"code"`
}
```

### Testing

#### Unit Tests

```go
func TestPackageUpload(t *testing.T) {
    // Arrange
    service := NewTestService()
    testFile := createTestRPM(t)
    defer os.Remove(testFile)

    // Act
    err := service.UploadPackage(context.Background(), "test-repo", testFile)

    // Assert
    assert.NoError(t, err)
    assert.FileExists(t, filepath.Join("storage", "test-repo", "Packages", testFile))
}
```

#### Integration Tests

```go
func TestAPIEndToEnd(t *testing.T) {
    // Start test server
    server := startTestServer(t)
    defer server.Close()

    // Test repository creation
    resp := createRepository(t, server.URL, "test-repo")
    assert.Equal(t, http.StatusOK, resp.StatusCode)

    // Test package upload
    uploadResp := uploadPackage(t, server.URL, "test-repo", "test.rpm")
    assert.Equal(t, http.StatusOK, uploadResp.StatusCode)
}
```

## 🏗️ Project Structure

```
plus/
├── cmd/
│   └── plus/           # Main application entry point
├── internal/
│   ├── api/           # HTTP handlers and routing
│   ├── config/        # Configuration management
│   ├── metrics/       # Metrics collection
│   ├── middleware/    # HTTP middleware
│   ├── service/       # Business logic
│   └── types/         # Data structures
├── pkg/
│   ├── client/        # Go client library
│   ├── repo/          # Repository implementations
│   └── storage/       # Storage backends
├── assets/            # Embedded static files
├── docs/              # Documentation
├── scripts/           # Build and deployment scripts
└── tests/             # Integration tests
```

## 🧪 Testing Guidelines

### Test Categories

1. **Unit Tests** - Test individual functions/methods
2. **Integration Tests** - Test component interactions
3. **API Tests** - Test HTTP endpoints
4. **Performance Tests** - Test under load

### Running Tests

```bash
# All tests
make test

# Unit tests only
go test ./internal/...

# Integration tests
go test ./tests/...

# With coverage
make test-coverage

# Specific package
go test ./internal/service -v

# With race detection
go test -race ./...
```

### Test Data

- Use `testdata/` directories for test files
- Clean up temporary files in tests
- Use table-driven tests for multiple scenarios

```go
func TestValidatePackage(t *testing.T) {
    tests := []struct {
        name     string
        filename string
        want     bool
        wantErr  bool
    }{
        {"valid RPM", "test.rpm", true, false},
        {"valid DEB", "test.deb", true, false},
        {"invalid extension", "test.txt", false, true},
        {"empty filename", "", false, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ValidatePackage(tt.filename)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidatePackage() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("ValidatePackage() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

## 📚 Documentation

### Code Documentation

- **Public functions** must have godoc comments
- **Complex logic** should be commented
- **Examples** in godoc when helpful

```go
// UploadPackage uploads a package file to the specified repository.
// It validates the package format, stores the file, and updates repository metadata.
//
// Example:
//   err := service.UploadPackage(ctx, "my-repo", "package.rpm", reader)
//   if err != nil {
//       log.Printf("Upload failed: %v", err)
//   }
func (s *Service) UploadPackage(ctx context.Context, repoName, filename string, reader io.Reader) error {
    // implementation
}
```

### API Documentation

- Update `docs/api.md` for API changes
- Include request/response examples
- Document error codes and messages

## 🚀 Release Process

### Version Numbering

We use [Semantic Versioning](https://semver.org/):

- **MAJOR** version for incompatible API changes
- **MINOR** version for backward-compatible functionality
- **PATCH** version for backward-compatible bug fixes

### Release Checklist

1. **Update version** in relevant files
2. **Update CHANGELOG.md** with changes
3. **Run full test suite**
4. **Update documentation**
5. **Create release PR**
6. **Tag release** after merge
7. **Build and publish** artifacts

## 🔧 Development Tools

### Required Tools

```bash
# Go toolchain
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Development tools
make install-tools
```

### Makefile Targets

```bash
make build          # Build binary
make test           # Run tests
make test-coverage  # Run tests with coverage
make lint           # Run linters
make fmt            # Format code
make clean          # Clean build artifacts
make docker-build   # Build Docker image
make docker-run     # Run in Docker
```

### IDE Setup

#### VS Code

Recommended extensions:
- Go (official)
- REST Client
- Docker

Settings:
```json
{
    "go.formatTool": "goimports",
    "go.lintTool": "golangci-lint",
    "go.testFlags": ["-v", "-race"]
}
```

## 🐛 Debugging

### Local Development

```bash
# Run with debug logging
go run ./cmd/plus --config config.dev.yaml --debug

# Run with race detection
go run -race ./cmd/plus --config config.dev.yaml

# Profile memory usage
go run ./cmd/plus --config config.dev.yaml --profile
```

### Common Issues

1. **Port already in use**
   ```bash
   lsof -i :8080
   kill -9 <PID>
   ```

2. **Permission denied on storage**
   ```bash
   chmod -R 755 storage/
   ```

3. **Module issues**
   ```bash
   go mod tidy
   go mod download
   ```

## 📞 Getting Help

- **GitHub Discussions** for questions and ideas
- **GitHub Issues** for bugs and feature requests
- **Code Review** for implementation feedback

## 🎉 Recognition

Contributors will be:
- Listed in CONTRIBUTORS.md
- Mentioned in release notes
- Invited to maintainer team (for significant contributions)

Thank you for contributing to Plus! 🚀