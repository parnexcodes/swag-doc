package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

// User represents a user in the system
type User struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// Post represents a blog post
type Post struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	UserID  int    `json:"user_id"`
}

var users = []User{
	{ID: 1, Username: "johndoe", Email: "john@example.com", FirstName: "John", LastName: "Doe"},
	{ID: 2, Username: "janedoe", Email: "jane@example.com", FirstName: "Jane", LastName: "Doe"},
}

var posts = []Post{
	{ID: 1, Title: "First Post", Content: "This is my first post", UserID: 1},
	{ID: 2, Title: "Second Post", Content: "This is my second post", UserID: 1},
	{ID: 3, Title: "Hello World", Content: "Hello, world!", UserID: 2},
}

func main() {
	// Define routes
	http.HandleFunc("/users", handleUsers)
	http.HandleFunc("/users/", handleUser)
	http.HandleFunc("/posts", handlePosts)
	http.HandleFunc("/posts/", handlePost)
	http.HandleFunc("/graphql", handleGraphQL)
	http.HandleFunc("/ws", handleWS)

	// Start server
	log.Println("Starting server on :3000")
	if err := http.ListenAndServe(":3000", nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Return all users
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(users)
	case http.MethodPost:
		// Create a new user
		var user User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Set a new ID (in a real app, this would be handled by a database)
		user.ID = len(users) + 1
		users = append(users, user)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(user)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleUser(w http.ResponseWriter, r *http.Request) {
	// Extract ID from URL
	id := extractID(r.URL.Path, "/users/")
	if id <= 0 || id > len(users) {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	user := users[id-1]

	switch r.Method {
	case http.MethodGet:
		// Return the user
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(user)
	case http.MethodPut:
		// Update the user
		var updatedUser User
		if err := json.NewDecoder(r.Body).Decode(&updatedUser); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Preserve the ID
		updatedUser.ID = user.ID
		users[id-1] = updatedUser

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(updatedUser)
	case http.MethodDelete:
		// Delete the user
		users = append(users[:id-1], users[id:]...)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handlePosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Return all posts
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(posts)
	case http.MethodPost:
		// Create a new post
		var post Post
		if err := json.NewDecoder(r.Body).Decode(&post); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Set a new ID
		post.ID = len(posts) + 1
		posts = append(posts, post)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(post)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
func handleGraphQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "GraphQL endpoint requires POST", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	query := strings.ToLower(request.Query)
	var data map[string]any
	switch {
	case strings.Contains(query, "createuser"):
		input, _ := request.Variables["input"].(map[string]any)
		user := User{
			ID:        len(users) + 1,
			Username:  stringVariable(input, "username"),
			Email:     stringVariable(input, "email"),
			FirstName: stringVariable(input, "first_name"),
			LastName:  stringVariable(input, "last_name"),
		}
		users = append(users, user)
		data = map[string]any{"createUser": user}
	case strings.Contains(query, "posts"):
		data = map[string]any{"posts": posts}
	case strings.Contains(query, "user"):
		id := intVariable(request.Variables, "id")
		if id <= 0 || id > len(users) {
			data = map[string]any{"user": nil}
		} else {
			data = map[string]any{"user": users[id-1]}
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]string{{"message": "unsupported example query"}},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func stringVariable(values map[string]any, name string) string {
	value, _ := values[name].(string)
	return value
}

func intVariable(values map[string]any, name string) int {
	switch value := values[name].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case string:
		var result int
		_, _ = fmt.Sscanf(value, "%d", &result)
		return result
	default:
		return 1
	}
}

func handlePost(w http.ResponseWriter, r *http.Request) {
	// Extract ID from URL
	id := extractID(r.URL.Path, "/posts/")
	if id <= 0 || id > len(posts) {
		http.Error(w, "Post not found", http.StatusNotFound)
		return
	}

	post := posts[id-1]

	switch r.Method {
	case http.MethodGet:
		// Return the post
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(post)
	case http.MethodPut:
		// Update the post
		var updatedPost Post
		if err := json.NewDecoder(r.Body).Decode(&updatedPost); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Preserve the ID
		updatedPost.ID = post.ID
		posts[id-1] = updatedPost

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(updatedPost)
	case http.MethodDelete:
		// Delete the post
		posts = append(posts[:id-1], posts[id:]...)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// extractID extracts the ID from a URL path
func extractID(path, prefix string) int {
	// This is a simple implementation, a more robust version would use regexp
	idStr := path[len(prefix):]
	var id int
	_, err := fmt.Sscanf(idStr, "%d", &id)
	if err != nil {
		return -1
	}
	return id
}

// handleWS upgrades the connection to a WebSocket and echoes every received
// frame back verbatim (the payload is unmasked for reading but re-sent as-is).
func handleWS(w http.ResponseWriter, r *http.Request) {
	if !isWebSocketUpgrade(r) {
		http.Error(w, "Expected WebSocket upgrade", http.StatusBadRequest)
		return
	}

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")
	w.Header().Set("Sec-WebSocket-Accept", wsAcceptKey(r.Header.Get("Sec-WebSocket-Key")))
	w.WriteHeader(http.StatusSwitchingProtocols)

	hj, ok := w.(http.Hijacker)
	if !ok {
		log.Println("WebSocket: ResponseWriter does not support hijacking")
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		log.Printf("WebSocket: hijack failed: %v", err)
		return
	}
	defer func() { _ = conn.Close() }()

	log.Println("WebSocket client connected")
	defer log.Println("WebSocket client disconnected")

	// Echo loop: clients send masked frames; unmask for inspection, then
	// forward the payload back unmasked (server-to-client frames are not
	// masked per RFC 6455).
	for {
		hdr, err := proxy.ReadWSFrameHeader(brw.Reader)
		if err != nil {
			return
		}
		payload := make([]byte, hdr.PayloadLen)
		if _, err := io.ReadFull(brw.Reader, payload); err != nil {
			return
		}
		if hdr.Masked {
			proxy.UnmaskBytes(payload, payload, hdr.MaskKey, 0)
		}
		if err := proxy.WriteWSFrame(conn, hdr.Fin, hdr.Opcode, payload, false); err != nil {
			return
		}
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	connectionOK := false
	for _, value := range r.Header.Values("Connection") {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
				connectionOK = true
			}
		}
	}
	if !connectionOK {
		return false
	}
	for _, part := range strings.Split(r.Header.Get("Upgrade"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "websocket") {
			return true
		}
	}
	return false
}

// wsAcceptKey computes the Sec-WebSocket-Accept handshake response value
// (RFC 6455 section 4.2.2).
func wsAcceptKey(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}
