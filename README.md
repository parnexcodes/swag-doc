# SwagDoc

![SwagDoc Logo](docs/logo.svg)

SwagDoc automatically generates Swagger/OpenAPI documentation from your API traffic without requiring any code changes.

## Features

- Acts as a proxy to capture API traffic
- Analyzes HTTP requests and responses to infer API structure
- Automatically detects data types, parameters, and response schemas
- Generates OpenAPI/Swagger documentation based on observed traffic
- No code changes required to your existing API
- Can be run as a standalone application

## Installation

### Using Go

```bash
go install github.com/parnexcodes/swag-doc/cmd/swagdoc@latest
```

### From Releases

You can download pre-built binaries from the [Releases](https://github.com/parnexcodes/swag-doc/releases) page.

### From Source

```bash
git clone https://github.com/parnexcodes/swag-doc.git
cd swag-doc
go build -o swagdoc ./cmd/swagdoc
```

## Usage

### As a Proxy

To use SwagDoc, you need to set it up as a proxy in front of your API:

```bash
swagdoc proxy --port 8080 --target http://your-api-server.com
```

This will start a proxy server on port 8080 that forwards requests to your API server and captures traffic for documentation.

### Generating Documentation

Once you have captured some API traffic, you can generate Swagger/OpenAPI documentation:

```bash
swagdoc generate --output swagger.json
```

### Options

#### Proxy Command

- `--port`: Port to run the proxy server on (default: 8080)
- `--target`: Target API server URL (required)
- `--data-dir`: Directory to store API transaction data (default: ./swagdoc-data)

#### Generate Command

- `--output`: Output file for Swagger documentation (default: swagger.json)
- `--data-dir`: Directory to read API transaction data from (default: ./swagdoc-data)
- `--title`: Title for the API documentation (default: "API Documentation")
- `--description`: Description for the API documentation (default: "Generated API documentation")
- `--version`: API version (default: "1.0.0")
- `--base-path`: Base path for the API (default: "http://localhost:8080")
- `--cleanup`: Delete the data directory after generating documentation (default: false)
- `--group-by-path`: Group API endpoints by path segments (default: true)
- `--tag-mapping`: Custom tag mappings in format 'path:tag' (can be used multiple times)
- `--version-prefix`: Custom version prefixes (can be used multiple times)

### Organizing API Documentation

SwagDoc automatically organizes your API endpoints into logical groups based on the URL path structure. For example:

- `/auth/login` and `/auth/register` will be grouped under the "Auth" tag
- `/users/123` and `/users/profile` will be grouped under the "Users" tag
- `/api/v1/orders` will be grouped under the "Orders" tag (automatically handling API version prefixes)

You can customize this grouping with the `--tag-mapping` flag:

```bash
# Group all paths starting with "auth" under "Authentication" tag
swagdoc generate --tag-mapping "auth:Authentication" --tag-mapping "users:User Management"

# Define custom version prefixes
swagdoc generate --version-prefix "api" --version-prefix "v4"

# Disable automatic path grouping
swagdoc generate --group-by-path=false
```

## How It Works

1. **Intercept Traffic**: SwagDoc acts as a proxy between clients and your API server, intercepting all HTTP requests and responses.
2. **Analyze Patterns**: It analyzes the structure of requests and responses, including:
   - URL paths and parameters
   - HTTP methods
   - Request/response bodies
   - Status codes
   - Content types
3. **Infer Types**: It infers data types from the observed values in JSON payloads.
4. **Generate OpenAPI**: It generates an OpenAPI specification that describes your API.

## Development

### Building From Source

```bash
# Build for your current platform
make build

# Run tests
make test

# Clean build artifacts
make clean
```

### GitHub Actions Workflows

This project includes GitHub Actions workflows for:

- **Testing**: Runs tests on every push and PR
- **Building**: Builds the application for Linux, Windows, and macOS
- **Releasing**: Creates a new GitHub release with compiled binaries when a new tag is pushed

To create a new release:

```bash
git tag v1.0.0
git push origin v1.0.0
```

## Future Features

- Framework-specific middleware (Express, Gin, FastAPI, etc.)
- Interactive UI for viewing and editing generated docs
- Path parameter detection and templating
- Authentication flow documentation
- WebSocket API documentation
- gRPC support
- GraphQL support

## Demo

See the [examples](examples/) directory for a demo API and instructions on how to use SwagDoc with it.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT 