.PHONY: build run test clean example proxy generate

# Build the application
build:
	go build -o bin/swagdoc ./cmd/swagdoc

# Run the tests
test:
	go test -v ./...

# Clean the build artifacts
clean:
	rm -rf bin/
	rm -rf swagdoc-data/

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
	@echo "Starting example API server..."
	@go run ./examples/simple_api/main.go & \
	API_PID=$$! && \
	echo "API server started with PID: $$API_PID" && \
	sleep 2 && \
	echo "Starting proxy server..." && \
	./bin/swagdoc proxy --port 8888 --target http://localhost:3000 & \
	PROXY_PID=$$! && \
	echo "Proxy server started with PID: $$PROXY_PID" && \
	sleep 2 && \
	echo "Making sample API requests..." && \
	curl -s -X GET http://localhost:8888/users > /dev/null && \
	curl -s -X GET http://localhost:8888/users/1 > /dev/null && \
	curl -s -X POST http://localhost:8888/users \
		-H "Content-Type: application/json" \
		-d '{"username":"bobsmith","email":"bob@example.com","first_name":"Bob","last_name":"Smith"}' > /dev/null && \
	curl -s -X GET http://localhost:8888/posts > /dev/null && \
	curl -s -X GET http://localhost:8888/posts/1 > /dev/null && \
	curl -s -X POST http://localhost:8888/posts \
		-H "Content-Type: application/json" \
		-d '{"title":"New Post","content":"This is a new post","user_id":1}' > /dev/null && \
	echo "Generating Swagger documentation..." && \
	./bin/swagdoc generate --output swagger.json --base-path http://localhost:3000 && \
	echo "Stopping servers..." && \
	kill $$PROXY_PID && \
	kill $$API_PID 