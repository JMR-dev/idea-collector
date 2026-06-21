package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func TestCreateIssueOnBoard(t *testing.T) {
	var gotIssueBody map[string]string
	var gotGraphQL map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
				t.Errorf("token request missing app JWT bearer: %q", got)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "ghs_installationtoken",
				"expires_at": "2999-01-01T00:00:00Z",
			})
		case strings.HasSuffix(r.URL.Path, "/issues"):
			if got := r.Header.Get("Authorization"); got != "token ghs_installationtoken" {
				t.Errorf("issue request wrong auth: %q", got)
			}
			_ = json.NewDecoder(r.Body).Decode(&gotIssueBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number":   42,
				"html_url": "https://github.com/o/r/issues/42",
				"node_id":  "I_issuenode",
			})
		case strings.HasSuffix(r.URL.Path, "/graphql"):
			_ = json.NewDecoder(r.Body).Decode(&gotGraphQL)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"addProjectV2ItemById": map[string]any{
						"item": map[string]any{"id": "PVTI_item"},
					},
				},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, err := New(123, 456, testKeyPEM(t), srv.URL, srv.URL+"/graphql")
	if err != nil {
		t.Fatal(err)
	}

	res, err := c.CreateIssueOnBoard(context.Background(), "o", "r", "PVT_board", "My idea", "body text")
	if err != nil {
		t.Fatalf("CreateIssueOnBoard: %v", err)
	}
	if res.IssueNumber != 42 || res.IssueNodeID != "I_issuenode" {
		t.Errorf("unexpected result: %+v", res)
	}
	if gotIssueBody["title"] != "My idea" || gotIssueBody["body"] != "body text" {
		t.Errorf("issue payload mismatch: %+v", gotIssueBody)
	}
	vars, _ := gotGraphQL["variables"].(map[string]any)
	if vars["projectId"] != "PVT_board" || vars["contentId"] != "I_issuenode" {
		t.Errorf("graphql variables mismatch: %+v", vars)
	}
}

func TestGraphQLErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/access_tokens") {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "t", "expires_at": "2999-01-01T00:00:00Z"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/issues") {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 1, "html_url": "u", "node_id": "n"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]any{{"message": "Projects v2 not enabled"}},
		})
	}))
	defer srv.Close()

	c, err := New(1, 2, testKeyPEM(t), srv.URL, srv.URL+"/graphql")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateIssueOnBoard(context.Background(), "o", "r", "PVT", "t", "b")
	if err == nil || !strings.Contains(err.Error(), "adding to board failed") {
		t.Errorf("expected board failure error, got %v", err)
	}
}
