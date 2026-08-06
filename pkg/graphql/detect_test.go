package graphql

import (
	"net/http"
	"testing"

	"github.com/parnexcodes/swag-doc/pkg/proxy"
)

func TestParseRequestJSON(t *testing.T) {
	transaction := proxy.APITransaction{
		Request: proxy.RequestData{
			Method:  http.MethodPost,
			Headers: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body:    []byte(`{"query":"query GetUser($id: ID!) { user(id: $id) { id } }","variables":{"id":"1"}}`),
		},
	}
	request, ok := ParseRequest(transaction)
	if !ok {
		t.Fatal("ParseRequest() did not detect JSON GraphQL")
	}
	if len(request.Document.Operations) != 1 || request.Document.Operations[0].Name != "GetUser" {
		t.Fatalf("document = %#v", request.Document)
	}
	if request.Variables["id"] != "1" {
		t.Fatalf("variables = %#v", request.Variables)
	}
}

func TestParseRequestRawGraphQL(t *testing.T) {
	transaction := proxy.APITransaction{
		Request: proxy.RequestData{
			Headers: http.Header{"Content-Type": []string{"application/graphql"}},
			Body:    []byte(`mutation CreateUser { createUser { id } }`),
		},
	}
	request, ok := ParseRequest(transaction)
	if !ok {
		t.Fatal("ParseRequest() did not detect application/graphql")
	}
	if request.Document.Operations[0].Kind != "mutation" {
		t.Fatalf("operation = %#v", request.Document.Operations[0])
	}
}

func TestParseRequestForm(t *testing.T) {
	transaction := proxy.APITransaction{
		Request: proxy.RequestData{
			Headers: http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}},
			Body:    []byte(`query=query+%7B+user+%7B+id+%7D+%7D&variables=%7B%22id%22%3A1%7D`),
		},
	}
	request, ok := ParseRequest(transaction)
	if !ok {
		t.Fatal("ParseRequest() did not detect form GraphQL")
	}
	if request.Variables["id"] != float64(1) {
		t.Fatalf("variables = %#v", request.Variables)
	}
}

func TestParseRequestRejectsNonGraphQL(t *testing.T) {
	cases := []proxy.APITransaction{
		{Request: proxy.RequestData{Method: "POST", Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"query":"search term"}`)}},
		{Request: proxy.RequestData{Method: "POST", Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"variables":{}}`)}},
		{Request: proxy.RequestData{Method: "POST", Headers: http.Header{"Content-Type": []string{"text/plain"}}, Body: []byte(`query { user { id } }`)}},
		{Request: proxy.RequestData{Method: "GET", Headers: http.Header{"Content-Type": []string{"application/json"}}}},
		{Request: proxy.RequestData{Method: "POST", Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"query":`)}},
	}
	for i, transaction := range cases {
		if _, ok := ParseRequest(transaction); ok {
			t.Errorf("case %d was detected as GraphQL", i)
		}
	}
}
