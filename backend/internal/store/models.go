package store

import "time"

type Project struct {
	ID              string
	Slug            string
	Name            string
	GitHubOwner     string
	GitHubRepo      string
	GitHubProjectID string
	CreatedAt       time.Time
}

type User struct {
	AuthCode    string
	ProjectID   string
	DisplayName string
	Email       *string
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

type Submission struct {
	ID                string
	AuthCode          string
	ProjectID         string
	Title             string
	Body              string
	Status            string
	GitHubIssueNumber *int
	GitHubIssueURL    *string
	GitHubNodeID      *string
	Error             *string
	CreatedAt         time.Time
}

const (
	SubmissionPending = "pending"
	SubmissionCreated = "created"
	SubmissionFailed  = "failed"
)
