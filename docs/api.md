## 4. docs/api.md

```markdown
# Plus Artifacts Server API Reference

This document provides comprehensive API documentation for Plus Artifacts Server.

## Base URL

```
http://localhost:8080
```

## Authentication

Currently, Plus does not require authentication. This will be added in future versions.

## Response Format

All API responses follow a consistent JSON format:

```json
{
  "status": {
    "status": "success|error",
    "message": "Human readable message",
    "code": 200
  },
  "data": {
    // Response data (varies by endpoint)
  }
}
```

## Error Handling

### HTTP Status Codes

- `200` - Success
- `400` - Bad Request
- `404` - Not Found
- `500` - Internal Server Error
- `503` - Service Unavailable

### Error Response Example

```json
{
  "status": "error",
  "message": "Repository not found",
  "code": 404
}
```

## Health & Monitoring

### Health Check

Check if the service is healthy.

**Endpoint:** `GET /health`

**Response:**
```json
{
  "status": "healthy",
  "server": "plus"
}
```

**Example:**
```bash
curl http://localhost:8080/health
```

### Readiness Check

Check if the service is ready to handle requests.

**Endpoint:** `GET /ready`

**Response:**
```json
{
  "status": {
    "status": "ready"
  },
  "checks": {
    "storage": "ok"
  }
}
```

**Example:**
```bash
curl http://localhost:8080/ready
```

### Metrics

Get detailed service metrics.

**Endpoint:** `GET /metrics`

**Response:**
```json
{
  "requests": {
    "total": 1250,
    "uploads": 45,
    "downloads": 890,
    "errors": 12,
    "active": 3
  },
  "performance": {
    "response_time_ms": 25,
    "goroutines": 15
  },
  "memory": {
    "alloc_mb": 45,
    "total_alloc_mb": 120,
    "sys_mb": 78,
    "gc_cycles": 8
  }
}
```

**Example:**
```bash
curl http://localhost:8080/metrics
```

## Repository Management

### List Repositories

Get a list of all repositories.

**Endpoint:** `GET /repos`

**Response:**
```json
{
  "status": {
    "server": "Plus",
    "status": "success",
    "code": 200
  },
  "repositories": [
    "centos/7",
    "ubuntu/20.04",
    "my-repo"
  ],
  "tree": {
    "centos": {
      "type": "directory",
      "children": {
        "7": {
          "type": "repo",
          "path": "centos/7"
        }
      }
    }
  },
  "count": 3
}
```

**Example:**
```bash
curl http://localhost:8080/repos
```

### Create Repository

Create a new repository.

**Endpoint:** `POST /repos`

**Request Body:**
```json
{
  "name": "my-repo",
  "path": "optional/sub/path"
}
```

**Response:**
```json
{
  "status": "success",
  "message": "Repository created successfully",
  "code": 200
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/repos \
  -H "Content-Type: application/json" \
  -d '{"name": "my-repo", "path": "centos/8"}'
```

### Get Repository Info

Get detailed information about a repository.

**Endpoint:** `GET /repo/{repoName}`

**Response:**
```json
{
  "status": {
    "status": "success"
  },
  "name": "my-repo",
  "package_count": 25,
  "rpm_count": 20,
  "deb_count": 5,
  "total_size": 1048576000,
  "packages": [
    {
      "name": "package1.rpm",
      "size": 1024000
    },
    {
      "name": "package2.deb",
      "size": 2048000
    }
  ]
}
```

**Example:**
```bash
curl http://localhost:8080/repo/my-repo
```

### Delete Repository

Delete a repository and all its packages.

**Endpoint:** `DELETE /repo/{repoName}`

**Response:**
```json
{
  "status": "success",
  "message": "Repository deleted successfully",
  "code": 200
}
```

**Example:**
```bash
curl -X DELETE http://localhost:8080/repo/my-repo
```

## Package Management

### Upload Package

Upload a single package to a repository.

**Endpoint:** `POST /repo/{repoName}/upload`

**Request:** Multipart form with file field

**Response:**
```json
{
  "status": "success",
  "message": "Package uploaded successfully",
  "code": 200
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/repo/my-repo/upload \
  -F "file=@package.rpm"
```

### Batch Upload

Upload multiple packages at once.

**Endpoint:** `POST /batch-upload`

**Request:** Multipart form with:
- `repository`: Repository name
- `files`: Multiple file fields
- `auto_refresh`: Optional, set to "true" to auto-refresh metadata

**Response:**
```json
{
  "status": "success",
  "total": 3,
  "success": 2,
  "failed": 1,
  "results": [
    {
      "filename": "package1.rpm",
      "status": "success"
    },
    {
      "filename": "package2.rpm",
      "status": "success"
    },
    {
      "filename": "invalid.txt",
      "status": "failed",
      "error": "Unsupported file type"
    }
  ]
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/batch-upload \
  -F "repository=my-repo" \
  -F "files=@package1.rpm" \
  -F "files=@package2.rpm" \
  -F "auto_refresh=true"
```

### Download Package

Download a package from a repository.

**Endpoint:** 
- `GET /repo/{repoName}/rpm/{filename}` (for RPM packages)
- `GET /repo/{repoName}/deb/{filename}` (for DEB packages)

**Response:** Binary file with appropriate headers

**Example:**
```bash
# Download RPM package
curl -O http://localhost:8080/repo/my-repo/rpm/package.rpm

# Download DEB package
curl -O http://localhost:8080/repo/my-repo/deb/package.deb
```

### Get Package Checksum

Get the SHA256 checksum of a package.

**Endpoint:** `GET /repo/{repoName}/checksum/{filename}`

**Response:**
```json
{
  "status": {
    "status": "success",
    "message": "Checksum retrieved successfully",
    "code": 200
  },
  "filename": "package.rpm",
  "sha256": "a1b2c3d4e5f6789...",
  "repo": "my-repo"
}
```

**Example:**
```bash
curl http://localhost:8080/repo/my-repo/checksum/package.rpm
```

## Repository Operations

### Refresh Metadata

Refresh repository metadata (repodata).

**Endpoint:** `POST /repo/{repoName}/refresh`

**Response:**
```json
{
  "status": {
    "status": "success",
    "message": "Repository metadata refreshed successfully"
  },
  "repo": "my-repo"
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/repo/my-repo/refresh
```

### Browse Repository Files

Browse repository files and directories.

**Endpoint:** `GET /repo/{repoName}/files/{path}`

**Response:** HTML directory listing or file content

**Example:**
```bash
# Browse repository root
curl http://localhost:8080/repo/my-repo/files/

# Browse specific directory
curl http://localhost:8080/repo/my-repo/files/Packages/

# Get specific file
curl http://localhost:8080/repo/my-repo/files/repodata/repomd.xml
```

### Get Metadata Files

Access repository metadata files directly.

**Endpoint:** `GET /repo/{repoName}/repodata/{filename}`

**Common metadata files:**
- `repomd.xml` - Repository metadata index
- `{hash}-primary.xml.gz` - Package information
- `{hash}-filelists.xml.gz` - File lists
- `{hash}-other.xml.gz` - Additional metadata

**Example:**
```bash
# Get repository metadata index
curl http://localhost:8080/repo/my-repo/repodata/repomd.xml

# Get primary metadata
curl http://localhost:8080/repo/my-repo/repodata/abc123-primary.xml.gz
```

## Multi-level Repository Paths

Plus supports multi-level repository paths for better organization:

**Examples:**
```bash
# Create nested repository
curl -X POST http://localhost:8080/repos \
  -H "Content-Type: application/json" \
  -d '{"name": "centos", "path": "7/x86_64"}'

# Upload to nested repository
curl -X POST http://localhost:8080/repo/centos/7/x86_64/upload \
  -F "file=@package.rpm"

# Download from nested repository
curl -O http://localhost:8080/repo/centos/7/x86_64/rpm/package.rpm
```

## YUM Repository Configuration

To use Plus repositories with YUM:

```bash
# Create repository configuration
cat > /etc/yum.repos.d/plus-repo.repo << EOF
[plus-repo]
name=Plus Repository
baseurl=http://your-server:8080/repo/my-repo/files/
enabled=1
gpgcheck=0
metadata_expire=300
EOF

# Update YUM cache
yum clean all
yum makecache
```

## APT Repository Configuration

For DEB packages (future support):

```bash
# Add repository
echo "deb http://your-server:8080/repo/my-repo/files/ ./" > /etc/apt/sources.list.d/plus-repo.list

# Update package cache
apt update
```

## Rate Limiting

Currently, Plus does not implement rate limiting. This will be added in future versions.

## CORS Support

Plus supports CORS for web applications. CORS is enabled by default for all origins in development mode.

## WebSocket Support

WebSocket support for real-time updates is planned for future versions.

## Pagination

For endpoints that return large datasets, pagination will be added in future versions:

```json
{
  "data": [...],
  "pagination": {
    "page": 1,
    "per_page": 50,
    "total": 1000,
    "total_pages": 20
  }
}
```

## API Versioning

Currently, Plus uses a single API version. Future versions will include API versioning:

```
/api/v1/repos
/api/v2/repos
```

## SDK and Client Libraries

### Go Client

```go
import "github.com/elastic-io/plus/pkg/client"

c := client.NewClient(client.ClientConfig{
    BaseURL: "http://localhost:8080",
    Timeout: 30 * time.Second,
})

// Upload package
err := c.UploadPackage("my-repo", "./package.rpm")
```

### CLI Tool

```bash
# Install CLI
go install github.com/elastic-io/plus/cmd/plus-cli@latest

# Use CLI
plus-cli --server http://localhost:8080 upload --repo my-repo --file package.rpm
```

## Examples

### Complete Workflow

```bash
# 1. Check service health
curl http://localhost:8080/health

# 2. Create repository
curl -X POST http://localhost:8080/repos \
  -H "Content-Type: application/json" \
  -d '{"name": "my-repo"}'

# 3. Upload package
curl -X POST http://localhost:8080/repo/my-repo/upload \
  -F "file=@package.rpm"

# 4. Refresh metadata
curl -X POST http://localhost:8080/repo/my-repo/refresh

# 5. Verify package
curl http://localhost:8080/repo/my-repo/checksum/package.rpm

# 6. Configure YUM
cat > /etc/yum.repos.d/my-repo.repo << EOF
[my-repo]
name=My Repository
baseurl=http://localhost:8080/repo/my-repo/files/
enabled=1
gpgcheck=0
EOF

# 7. Use with YUM
yum install package-name
```

## Troubleshooting

### Common Error Codes

- `400` - Invalid request format or missing parameters
- `404` - Repository or package not found
- `500` - Internal server error (check logs)
- `503` - Service not ready (storage issues)

### Debug Mode

Enable debug logging for detailed API request/response information:

```bash
./plus --config config.yaml --debug
```

This API reference covers all current endpoints. For the latest updates, check the [GitHub repository](https://github.com/elastic-io/plus).