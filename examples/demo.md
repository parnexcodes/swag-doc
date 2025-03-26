# SwagDoc Demo

This demo shows how to use SwagDoc to automatically generate Swagger/OpenAPI documentation for an API.

## Prerequisites

- Go 1.19 or later
- cURL or any API testing tool

## Steps

1. Start the example API server:

```bash
cd examples/simple_api
go run main.go
```

This will start a simple API server on port 3000 with the following endpoints:

- `GET /users` - List all users
- `POST /users` - Create a user
- `GET /users/{id}` - Get a user by ID
- `PUT /users/{id}` - Update a user
- `DELETE /users/{id}` - Delete a user
- `GET /posts` - List all posts
- `POST /posts` - Create a post
- `GET /posts/{id}` - Get a post by ID
- `PUT /posts/{id}` - Update a post
- `DELETE /posts/{id}` - Delete a post

2. In a new terminal, start the SwagDoc proxy:

```bash
go run cmd/swagdoc/main.go proxy --port 8888 --target http://localhost:3000
```

3. Make requests to the API through the proxy:

```bash
# Get all users
curl -X GET http://localhost:8888/users

# Get a specific user
curl -X GET http://localhost:8888/users/1

# Create a new user
curl -X POST http://localhost:8888/users \
  -H "Content-Type: application/json" \
  -d '{"username":"bobsmith","email":"bob@example.com","first_name":"Bob","last_name":"Smith"}'

# Update a user
curl -X PUT http://localhost:8888/users/1 \
  -H "Content-Type: application/json" \
  -d '{"username":"johndoe","email":"john.updated@example.com","first_name":"John","last_name":"Doe"}'

# Get all posts
curl -X GET http://localhost:8888/posts

# Get a specific post
curl -X GET http://localhost:8888/posts/1

# Create a new post
curl -X POST http://localhost:8888/posts \
  -H "Content-Type: application/json" \
  -d '{"title":"New Post","content":"This is a new post","user_id":1}'

# Update a post
curl -X PUT http://localhost:8888/posts/1 \
  -H "Content-Type: application/json" \
  -d '{"title":"Updated Post","content":"This post has been updated","user_id":1}'
```

4. Generate the Swagger documentation:

```bash
go run cmd/swagdoc/main.go generate --output swagger.json --base-path http://localhost:3000
```

To generate documentation and automatically clean up the transaction data:

```bash
go run cmd/swagdoc/main.go generate --output swagger.json --base-path http://localhost:3000 --cleanup
```

### Organizing API Documentation with Tags

SwagDoc automatically organizes your API endpoints into logical groups based on the URL path structure. In this example, the endpoints are already organized into "Users" and "Posts" categories.

You can customize this grouping with the `--tag-mapping` flag:

```bash
# Group all users endpoints under "User Management" tag
go run cmd/swagdoc/main.go generate --output swagger.json --tag-mapping "users:User Management"

# Group multiple paths with descriptive tags
go run cmd/swagdoc/main.go generate --output swagger.json \
  --tag-mapping "users:User Management" \
  --tag-mapping "posts:Content Management"

# Define custom version prefixes (useful for APIs with versioning)
go run cmd/swagdoc/main.go generate --output swagger.json --version-prefix "api" --version-prefix "v2"
```

### Advanced Customization

For complex APIs with nested resources, you can map specific path prefixes to custom tags:

```bash
# For APIs with paths like /api/v1/users/1/posts
go run cmd/swagdoc/main.go generate --output swagger.json \
  --tag-mapping "users:User Management" \
  --tag-mapping "users/posts:User Content" \
  --version-prefix "api"
```

This will properly organize the Swagger UI into meaningful sections that make your API documentation more readable and easier to navigate.

5. View the generated documentation:

You can use tools like [Swagger UI](https://swagger.io/tools/swagger-ui/) or [Redoc](https://github.com/Redocly/redoc) to view the generated documentation.

For a quick preview, you can use the Swagger Editor:

1. Go to [https://editor.swagger.io/](https://editor.swagger.io/)
2. File -> Import File -> Select your generated `swagger.json` file

## Expected Results

The generated Swagger documentation should include:

- All the API endpoints
- Request and response schemas
- Example values based on the actual data
- HTTP methods and status codes

This documentation is generated without changing any code in the API itself, just by capturing and analyzing the traffic. 