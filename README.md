# SwagDoc

![SwagDoc Logo](docs/logo.svg)

SwagDoc automatically generates Swagger/OpenAPI documentation from your API traffic without requiring any code changes.

## Features

- Acts as a reverse proxy to capture API traffic
- Analyzes HTTP requests and responses to infer API structure
- Automatically detects data types, parameters, and response schemas
- Path parameter detection and templating (numeric IDs, UUIDs)
- Authentication flow detection (Bearer, API keys)
- Schema merging across multiple observations
- Generates OpenAPI 3.0 documentation based on observed traffic
- No code changes required to your existing API
- Graceful shutdown with signal handling

## Installation

### Using Go

```bash
go install github.com/parnexcodes/swag-doc/cmd/swagdoc@latest
```

### From Releases

Download pre-built binaries from the [Releases](https://github.com/parnexcodes/swag-doc/releases) page. Binaries are available for Linux, macOS, and Windows (amd64/arm64).

### From Source

```bash
git clone https://github.com/parnexcodes/swag-doc.git
cd swag-doc
make build
```

## Usage

### As a Proxy

Start SwagDoc as a proxy in front of your API:

```bash
swagdoc proxy --port 8080 --target http://your-api-server.com
```

This starts a proxy server that forwards requests to your API server and captures traffic for documentation. Press `Ctrl+C` to stop gracefully.

### Generating Documentation

Once you have captured API traffic, generate OpenAPI documentation:

```bash
swagdoc generate --output swagger.json
```

### CLI Reference

#### Proxy Command

| Flag | Description | Default |
|------|-------------|---------|
| `--port` | Port to run the proxy server on | `8080` |
| `--target` | Target API server URL (required) | |
| `--data-dir` | Directory to store API transaction data | `./swagdoc-data` |
| `--max-body-bytes` | Maximum response body size (in bytes) captured per transaction; the full response is still forwarded to the client | `10485760` (10 MiB) |

#### Generate Command

| Flag | Description | Default |
|------|-------------|---------|
| `--output` | Output file for documentation | `swagger.json` |
| `--data-dir` | Directory to read transaction data from | `./swagdoc-data` |
| `--title` | Title for the API documentation | `API Documentation` |
| `--description` | Description for the API documentation | `Generated API documentation` |
| `--version` | API version | `1.0.0` |
| `--base-path` | Base path for the API | `http://localhost:8080` |
| `--cleanup` | Delete the data directory after generating | `false` |
| `--group-by-path` | Group API endpoints by path segments | `true` |
| `--tag-mapping` | Custom tag mappings in format `path:tag` | |
| `--version-prefix` | Custom version prefixes | |

#### Version Command

```bash
swagdoc version
# swagdoc dev (commit: none, built: unknown)
```

### Organizing API Documentation

SwagDoc automatically organizes endpoints into logical groups based on URL path structure:

- `/auth/login` and `/auth/register` → "Auth" tag
- `/users/123` and `/users/profile` → "Users" tag
- `/api/v1/orders` → "Orders" tag (handles version prefixes)

Customize grouping:

```bash
swagdoc generate \
  --tag-mapping "auth:Authentication" \
  --tag-mapping "users:User Management" \
  --version-prefix "api" \
  --version-prefix "v4"
```

## How It Works

1. **Capture**: SwagDoc acts as a reverse proxy, intercepting all HTTP requests and responses.
2. **Store**: Raw transaction data is stored on disk. Sensitive headers and query parameters are redacted. Response bodies larger than `--max-body-bytes` are truncated for capture (the client still receives the full response), and transactions are flushed to disk periodically rather than on every request.
3. **Analyze**: Path patterns, authentication schemes, and data types are detected from the captured traffic.
4. **Generate**: An OpenAPI 3.0 specification is generated from the analyzed data, with schema merging across multiple observations.

## Development

### Building

```bash
make build       # Build binary
make test        # Run tests with race detector
make lint        # Run golangci-lint
make fmt         # Format code
make coverage    # Generate coverage report
make vulncheck   # Run vulnerability scanner
make clean       # Clean artifacts
```

### CI/CD

The project uses GitHub Actions:

- **CI** (`ci.yml`): Runs on every push/PR to master — lint, vulnerability check, tests (Linux/macOS/Windows), build
- **Release** (`release.yml`): Runs on version tags (`v*`) — cross-compiles via GoReleaser, creates GitHub release with checksums

To create a release:

```bash
git tag v1.0.0
git push origin v1.0.0
```

### Demo

```bash
make demo
```

This starts the example API, proxies to it, sends sample requests, generates `swagger.json`, and stops the servers.

## Future Features

- Framework-specific middleware (Express, Gin, FastAPI, etc.)
- Interactive UI for viewing and editing generated docs
- YAML output
- WebSocket API documentation
- gRPC support
- GraphQL support

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT
