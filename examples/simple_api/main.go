package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
		json.NewEncoder(w).Encode(users)
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
		json.NewEncoder(w).Encode(user)
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
		json.NewEncoder(w).Encode(user)
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
		json.NewEncoder(w).Encode(updatedUser)
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
		json.NewEncoder(w).Encode(posts)
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
		json.NewEncoder(w).Encode(post)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
		json.NewEncoder(w).Encode(post)
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
		json.NewEncoder(w).Encode(updatedPost)
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
