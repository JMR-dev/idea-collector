// Package github talks to GitHub as a GitHub App: it mints an installation token,
// creates issues via REST, and adds them to a Projects v2 board via GraphQL.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Result is the outcome of publishing a submission to GitHub.
type Result struct {
	IssueNumber int
	IssueURL    string
	IssueNodeID string
}

type Client struct {
	appID          int64
	installationID int64
	signKey        *rsa.PrivateKey
	apiBase        string
	graphqlURL     string
	http           *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// New builds a Client from the GitHub App credentials.
func New(appID, installationID int64, privateKeyPEM []byte, apiBase, graphqlURL string) (*Client, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing app private key: %w", err)
	}
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	if graphqlURL == "" {
		graphqlURL = "https://api.github.com/graphql"
	}
	return &Client{
		appID:          appID,
		installationID: installationID,
		signKey:        key,
		apiBase:        apiBase,
		graphqlURL:     graphqlURL,
		http:           &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// CreateIssueOnBoard creates an issue in owner/repo and adds it to the given
// ProjectV2 (node id). The project step is best-effort-reported via the error.
func (c *Client) CreateIssueOnBoard(ctx context.Context, owner, repo, projectNodeID, title, body string) (Result, error) {
	token, err := c.installationToken(ctx)
	if err != nil {
		return Result{}, err
	}

	res, err := c.createIssue(ctx, token, owner, repo, title, body)
	if err != nil {
		return Result{}, err
	}
	if err := c.addToProject(ctx, token, projectNodeID, res.IssueNodeID); err != nil {
		// Issue exists; surface the board failure so the caller can retry that step.
		return res, fmt.Errorf("issue #%d created but adding to board failed: %w", res.IssueNumber, err)
	}
	return res, nil
}

// ResolveProjectNodeID finds the ProjectV2 node id for an org or user project number.
func (c *Client) ResolveProjectNodeID(ctx context.Context, login string, number int) (string, error) {
	token, err := c.installationToken(ctx)
	if err != nil {
		return "", err
	}
	query := `query($login:String!,$number:Int!){
		organization(login:$login){ projectV2(number:$number){ id } }
		user(login:$login){ projectV2(number:$number){ id } }
	}`
	var resp struct {
		Data struct {
			Organization *struct {
				ProjectV2 *struct{ ID string } `json:"projectV2"`
			} `json:"organization"`
			User *struct {
				ProjectV2 *struct{ ID string } `json:"projectV2"`
			} `json:"user"`
		} `json:"data"`
		Errors []graphqlError `json:"errors"`
	}
	if err := c.graphql(ctx, token, query, map[string]any{"login": login, "number": number}, &resp); err != nil {
		return "", err
	}
	if resp.Data.Organization != nil && resp.Data.Organization.ProjectV2 != nil {
		return resp.Data.Organization.ProjectV2.ID, nil
	}
	if resp.Data.User != nil && resp.Data.User.ProjectV2 != nil {
		return resp.Data.User.ProjectV2.ID, nil
	}
	return "", fmt.Errorf("project number %d not found for %q", number, login)
}

func (c *Client) createIssue(ctx context.Context, token, owner, repo, title, body string) (Result, error) {
	payload, _ := json.Marshal(map[string]string{"title": title, "body": body})
	url := fmt.Sprintf("%s/repos/%s/%s/issues", c.apiBase, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return Result{}, apiError("create issue", resp)
	}
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		NodeID  string `json:"node_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("decoding issue response: %w", err)
	}
	return Result{IssueNumber: out.Number, IssueURL: out.HTMLURL, IssueNodeID: out.NodeID}, nil
}

func (c *Client) addToProject(ctx context.Context, token, projectNodeID, contentNodeID string) error {
	mutation := `mutation($projectId:ID!,$contentId:ID!){
		addProjectV2ItemById(input:{projectId:$projectId,contentId:$contentId}){ item { id } }
	}`
	var resp struct {
		Errors []graphqlError `json:"errors"`
	}
	return c.graphql(ctx, token, mutation,
		map[string]any{"projectId": projectNodeID, "contentId": contentNodeID}, &resp)
}

// installationToken returns a cached installation access token, refreshing when stale.
func (c *Client) installationToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-time.Minute)) {
		return c.token, nil
	}

	appJWT, err := c.appJWT()
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiBase, c.installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", apiError("installation token", resp)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}
	c.token, c.tokenExp = out.Token, out.ExpiresAt
	return c.token, nil
}

func (c *Client) appJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    strconv.FormatInt(c.appID, 10),
		IssuedAt:  jwt.NewNumericDate(now.Add(-30 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return tok.SignedString(c.signKey)
}

func (c *Client) graphql(ctx context.Context, token, query string, vars map[string]any, out any) error {
	payload, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return apiError("graphql", resp)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding graphql response: %w", err)
	}
	// Surface top-level GraphQL errors if the out struct exposes them.
	if errs := extractErrors(body); len(errs) > 0 {
		return fmt.Errorf("graphql error: %s", errs[0].Message)
	}
	return nil
}

type graphqlError struct {
	Message string `json:"message"`
}

func extractErrors(body []byte) []graphqlError {
	var e struct {
		Errors []graphqlError `json:"errors"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Errors
}

func apiError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("github %s: status %d: %s", op, resp.StatusCode, bytes.TrimSpace(body))
}
