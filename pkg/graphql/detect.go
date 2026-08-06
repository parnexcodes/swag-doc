package graphql

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

// ParsedRequest is a captured HTTP request identified as GraphQL.
type ParsedRequest struct {
	Document  *Document
	Variables map[string]any
}

// ParseRequest detects and parses GraphQL carried by one captured HTTP
// transaction. Non-GraphQL traffic returns ok=false.
func ParseRequest(transaction proxy.APITransaction) (*ParsedRequest, bool) {
	body := transaction.Request.Body
	if len(body) == 0 {
		return nil, false
	}

	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(
		transaction.Request.Headers.Get("Content-Type"), ";", 2)[0]))
	var queryText string
	var variables map[string]any

	switch {
	case mediaType == "application/graphql":
		queryText = string(body)
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Query == "" {
			return nil, false
		}
		queryText = payload.Query
		variables = payload.Variables
	case mediaType == "application/x-www-form-urlencoded":
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, false
		}
		queryText = form.Get("query")
		if queryText == "" {
			return nil, false
		}
		if encoded := form.Get("variables"); encoded != "" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(encoded), &parsed); err == nil {
				variables = parsed
			}
		}
	default:
		return nil, false
	}

	document, err := ParseDocument(queryText)
	if err != nil || len(document.Operations) == 0 {
		return nil, false
	}
	return &ParsedRequest{Document: document, Variables: variables}, true
}
