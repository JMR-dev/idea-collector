package store_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jasonross/idea-collect/backend/internal/authcode"
	"github.com/jasonross/idea-collect/backend/internal/store"
)

// Runs only when TEST_DATABASE_URL points at a reachable Postgres, e.g.:
//
//	TEST_DATABASE_URL=postgres://idea:idea@localhost:5432/idea?sslmode=disable go test ./internal/store -run Integration
func TestIntegrationLifecycle(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run store integration tests")
	}

	ctx := context.Background()
	st, err := store.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	slug := "it-" + authcode.New()
	project, err := st.CreateProject(ctx, slug, "Integration", "octo", "repo", "PVT_test")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	code := authcode.New()
	email := "jane@example.com"
	if _, err := st.CreateUser(ctx, code, project.ID, "Jane Doe", &email); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	user, gotProject, err := st.GetActiveUser(ctx, code)
	if err != nil {
		t.Fatalf("GetActiveUser: %v", err)
	}
	if user.DisplayName != "Jane Doe" || gotProject.ID != project.ID {
		t.Fatalf("unexpected user/project: %+v / %+v", user, gotProject)
	}

	subID, err := st.CreateSubmission(ctx, code, project.ID, "My idea", "details")
	if err != nil {
		t.Fatalf("CreateSubmission: %v", err)
	}

	pending, err := st.ListUnpublished(ctx, 100)
	if err != nil {
		t.Fatalf("ListUnpublished: %v", err)
	}
	if !containsSubmission(pending, subID) {
		t.Fatalf("new submission %s should be unpublished", subID)
	}

	if err := st.MarkSubmissionCreated(ctx, subID, 7, "https://example/7", "I_node"); err != nil {
		t.Fatalf("MarkSubmissionCreated: %v", err)
	}
	after, _ := st.ListUnpublished(ctx, 100)
	if containsSubmission(after, subID) {
		t.Fatalf("created submission %s should no longer be unpublished", subID)
	}

	// Revoked users cannot authenticate but are still retrievable for retries.
	if err := st.RevokeUser(ctx, code); err != nil {
		t.Fatalf("RevokeUser: %v", err)
	}
	if _, _, err := st.GetActiveUser(ctx, code); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked user should be ErrNotFound, got %v", err)
	}
	if _, err := st.GetUserByCode(ctx, code); err != nil {
		t.Fatalf("GetUserByCode (revoked): %v", err)
	}
}

func containsSubmission(subs []store.Submission, id string) bool {
	for _, s := range subs {
		if s.ID == id {
			return true
		}
	}
	return false
}
