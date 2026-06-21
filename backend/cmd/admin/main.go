// Command admin is the operator CLI for provisioning projects, users (invite codes),
// and retrying failed GitHub submissions. Run it on the host over SSH.
//
// Usage:
//
//	admin project create --slug demo --name "Demo" --owner org --repo demo-fb --project-number 1
//	admin project list
//	admin user create --project demo --name "Jane Doe" --email jane@example.com
//	admin user revoke --code ABCD-1234-WXYZ
//	admin user list --project demo
//	admin submission retry --limit 50
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/jasonross/idea-collect/backend/internal/authcode"
	"github.com/jasonross/idea-collect/backend/internal/config"
	"github.com/jasonross/idea-collect/backend/internal/github"
	"github.com/jasonross/idea-collect/backend/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		usage()
		return errors.New("expected: admin <project|user|submission> <subcommand>")
	}
	group, sub, rest := args[0], args[1], args[2:]

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	a := &app{cfg: cfg, st: st}
	switch group + " " + sub {
	case "project create":
		return a.projectCreate(ctx, rest)
	case "project list":
		return a.projectList(ctx)
	case "user create":
		return a.userCreate(ctx, rest)
	case "user revoke":
		return a.userRevoke(ctx, rest)
	case "user list":
		return a.userList(ctx, rest)
	case "submission retry":
		return a.submissionRetry(ctx, rest)
	default:
		usage()
		return fmt.Errorf("unknown command %q", group+" "+sub)
	}
}

type app struct {
	cfg *config.Config
	st  *store.Store
}

func (a *app) github() (*github.Client, error) {
	if !a.cfg.GitHub.Configured() {
		return nil, errors.New("GitHub App not configured (set GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, GITHUB_APP_PRIVATE_KEY_FILE)")
	}
	return github.New(a.cfg.GitHub.AppID, a.cfg.GitHub.InstallationID, a.cfg.GitHub.PrivateKeyPEM,
		a.cfg.GitHub.APIBase, a.cfg.GitHub.GraphQLURL)
}

func (a *app) projectCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("project create", flag.ContinueOnError)
	slug := fs.String("slug", "", "unique project slug")
	name := fs.String("name", "", "display name shown to submitters")
	owner := fs.String("owner", "", "GitHub org/user that owns the repo + board")
	repo := fs.String("repo", "", "GitHub repository for issues")
	nodeID := fs.String("project-node-id", "", "ProjectV2 node id (PVT_...); skips lookup")
	number := fs.Int("project-number", 0, "ProjectV2 number (resolved via the GitHub App)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" || *name == "" || *owner == "" || *repo == "" {
		return errors.New("--slug, --name, --owner and --repo are required")
	}

	projectID := *nodeID
	if projectID == "" {
		if *number == 0 {
			return errors.New("provide --project-node-id or --project-number")
		}
		gh, err := a.github()
		if err != nil {
			return err
		}
		projectID, err = gh.ResolveProjectNodeID(ctx, *owner, *number)
		if err != nil {
			return err
		}
	}

	p, err := a.st.CreateProject(ctx, *slug, *name, *owner, *repo, projectID)
	if err != nil {
		return err
	}
	fmt.Printf("created project %s (%s)\n  board node id: %s\n", p.Slug, p.ID, p.GitHubProjectID)
	return nil
}

func (a *app) projectList(ctx context.Context) error {
	projects, err := a.st.ListProjects(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tNAME\tREPO\tBOARD")
	for _, p := range projects {
		fmt.Fprintf(tw, "%s\t%s\t%s/%s\t%s\n", p.Slug, p.Name, p.GitHubOwner, p.GitHubRepo, p.GitHubProjectID)
	}
	return tw.Flush()
}

func (a *app) userCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user create", flag.ContinueOnError)
	projectSlug := fs.String("project", "", "project slug")
	name := fs.String("name", "", "submitter display name")
	email := fs.String("email", "", "optional email for reference")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *projectSlug == "" || *name == "" {
		return errors.New("--project and --name are required")
	}

	project, err := a.st.GetProjectBySlug(ctx, *projectSlug)
	if err != nil {
		return fmt.Errorf("project %q: %w", *projectSlug, err)
	}
	var emailPtr *string
	if *email != "" {
		emailPtr = email
	}

	code := authcode.New()
	if _, err := a.st.CreateUser(ctx, code, project.ID, *name, emailPtr); err != nil {
		return err
	}
	fmt.Printf("created invite for %s on %s\n  auth code: %s\n", *name, project.Slug, code)
	return nil
}

func (a *app) userRevoke(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user revoke", flag.ContinueOnError)
	code := fs.String("code", "", "auth code to revoke")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *code == "" {
		return errors.New("--code is required")
	}
	if err := a.st.RevokeUser(ctx, authcode.Normalize(*code)); err != nil {
		return err
	}
	fmt.Println("revoked")
	return nil
}

func (a *app) userList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user list", flag.ContinueOnError)
	projectSlug := fs.String("project", "", "project slug")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *projectSlug == "" {
		return errors.New("--project is required")
	}
	project, err := a.st.GetProjectBySlug(ctx, *projectSlug)
	if err != nil {
		return err
	}
	users, err := a.st.ListUsers(ctx, project.ID)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "CODE\tNAME\tEMAIL\tSTATUS")
	for _, u := range users {
		status := "active"
		if u.RevokedAt != nil {
			status = "revoked"
		}
		email := ""
		if u.Email != nil {
			email = *u.Email
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", u.AuthCode, u.DisplayName, email, status)
	}
	return tw.Flush()
}

func (a *app) submissionRetry(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("submission retry", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "max submissions to retry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	gh, err := a.github()
	if err != nil {
		return err
	}
	subs, err := a.st.ListUnpublished(ctx, *limit)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		fmt.Println("nothing to retry")
		return nil
	}

	var ok, failed int
	for _, sub := range subs {
		project, err := a.st.GetProjectByID(ctx, sub.ProjectID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "submission %s: project lookup failed: %v\n", sub.ID, err)
			failed++
			continue
		}
		user, err := a.st.GetUserByCode(ctx, sub.AuthCode)
		name := sub.AuthCode
		if err == nil {
			name = user.DisplayName
		}
		body := "**Submitted by:** " + name + "\n\n---\n\n" + sub.Body
		res, err := gh.CreateIssueOnBoard(ctx, project.GitHubOwner, project.GitHubRepo, project.GitHubProjectID, sub.Title, body)
		if err != nil {
			_ = a.st.MarkSubmissionFailed(ctx, sub.ID, err.Error())
			fmt.Fprintf(os.Stderr, "submission %s: %v\n", sub.ID, err)
			failed++
			continue
		}
		if err := a.st.MarkSubmissionCreated(ctx, sub.ID, res.IssueNumber, res.IssueURL, res.IssueNodeID); err != nil {
			fmt.Fprintf(os.Stderr, "submission %s: created issue but DB update failed: %v\n", sub.ID, err)
		}
		ok++
	}
	fmt.Printf("retried: %d succeeded, %d failed\n", ok, failed)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `idea-collect admin CLI

Commands:
  project create --slug --name --owner --repo (--project-node-id | --project-number)
  project list
  user create --project <slug> --name <name> [--email <email>]
  user revoke --code <auth-code>
  user list --project <slug>
  submission retry [--limit N]`)
}
