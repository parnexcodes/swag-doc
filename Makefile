.PHONY: build run test lint fmt coverage clean example proxy generate install demo vulncheck

# Build the application
build:
	go build -trimpath -ldflags="-s -w" -o bin/swagdoc ./cmd/swagdoc

# Run the tests
test:
	go test -v -race ./...

# Run golangci-lint
lint:
	golangci-lint run

# Format code
fmt:
	golangci-lint fmt

# Run tests with coverage
coverage:
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# Run vulnerability check
vulncheck:
	govulncheck ./...

# Clean the build artifacts
clean:
	rm -rf bin/
	rm -rf swagdoc-data/
	rm -f coverage.out coverage.html

# Run the example API
example:
	go run ./examples/simple_api/main.go

# Run the proxy
proxy: build
	./bin/swagdoc proxy --port 8888 --target http://localhost:3000

# Generate documentation
generate: build
	./bin/swagdoc generate --output swagger.json --base-path http://localhost:3000

# Install the application
install:
	go install ./cmd/swagdoc

# Full demo: Run the example API, proxy, and generate documentation
demo: build
	@go build -o bin/simple-api ./examples/simple_api
	@echo "Starting example API server..."
	@./bin/simple-api & \
	API_PID=$$!; \
	echo "API server started with PID: $$API_PID" && \
	sleep 2 && \
	echo "Starting proxy server..." && \
	./bin/swagdoc proxy --port 8888 --target http://localhost:3000 & \
	PROXY_PID=$$!; \
	echo "Proxy server started with PID: $$PROXY_PID" && \
	trap 'if [ -n "$$PROXY_PID" ]; then kill "$$PROXY_PID" 2>/dev/null || true; fi; if [ -n "$$API_PID" ]; then kill "$$API_PID" 2>/dev/null || true; fi' EXIT && \
	sleep 2 && \
	echo "Making sample API requests..." && \
	curl --retry 10 --retry-delay 1 --retry-connrefused --retry-all-errors -s -f -X GET http://localhost:8888/users > /dev/null && \
	curl -s -X GET http://localhost:8888/users/1 > /dev/null && \
	curl -s -X POST http://localhost:8888/users \
		-H "Content-Type: application/json" \
		-d '{"username":"bobsmith","email":"bob@example.com","first_name":"Bob","last_name":"Smith"}' > /dev/null && \
	curl -s -X GET http://localhost:8888/posts > /dev/null && \
	curl -s -X GET http://localhost:8888/posts/1 > /dev/null && \
	curl -s -X POST http://localhost:8888/posts \
		-H "Content-Type: application/json" \
		-d '{"title":"New Post","content":"This is a new post","user_id":1}' > /dev/null && \
	echo "Exercising GraphQL endpoint..." && \
	curl -s -X POST http://localhost:8888/graphql \
		-H "Content-Type: application/json" \
		-d '{"query":"query GetUser($$id: ID!) { user(id: $$id) { id username email } }","variables":{"id":"1"}}' > /dev/null && \
	curl -s -X POST http://localhost:8888/graphql \
		-H "Content-Type: application/json" \
		-d '{"query":"query ListPosts { posts { id title content user_id } }"}' > /dev/null && \
	curl -s -X POST http://localhost:8888/graphql \
		-H "Content-Type: application/json" \
		-d '{"query":"mutation CreateUser($$input: UserInput!) { createUser(input: $$input) { id username email } }","variables":{"input":{"username":"graphql-user","email":"graphql@example.com"}}}' > /dev/null && \
	echo "Exercising WebSocket endpoint..." && \
	go run ./examples/ws_client/main.go && \
	echo "Stopping proxy server..." && \
	if [ -n "$$PROXY_PID" ]; then kill "$$PROXY_PID" 2>/dev/null || true; wait "$$PROXY_PID" 2>/dev/null || true; fi && \
	echo "Generating Swagger documentation..." && \
	./bin/swagdoc generate --output swagger.json --base-path http://localhost:3000 && \
	echo "Generating AsyncAPI documentation..." && \
	./bin/swagdoc generate --format asyncapi --output asyncapi.json --base-path http://localhost:3000 && \
	echo "Generating GraphQL SDL documentation..." && \
	./bin/swagdoc generate --format graphql --output schema.graphql --base-path http://localhost:3000 && \
	echo "Stopping API server..." && \
	if [ -n "$$API_PID" ]; then kill "$$API_PID" 2>/dev/null || true; wait "$$API_PID" 2>/dev/null || true; fi
