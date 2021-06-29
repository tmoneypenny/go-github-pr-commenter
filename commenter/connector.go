package commenter

import (
	"context"
	"log"
	"time"

	"github.com/google/go-github/v32/github"
	"golang.org/x/oauth2"
)

type connector struct {
	prs      *github.PullRequestsService
	comments *github.IssuesService
	owner    string
	repo     string
	prNumber int
}

type existingComment struct {
	filename  *string
	comment   *string
	commentId *int64
}

func createConnector(input ConnectorInput) (*connector, error) {
	var client *github.Client
	var err error
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: input.Token})
	tc := oauth2.NewClient(ctx, ts)

	if input.EnterpriseConnectorInput != nil {
		client, err = github.NewEnterpriseClient(
			input.EnterpriseConnectorInput.BaseURL,
			input.EnterpriseConnectorInput.UploadURL,
			tc,
		)
		if err != nil {
			log.Println("could not establish a GHE connection")
			return nil, err
		}
	} else {
		client = github.NewClient(tc)
	}

	return &connector{
		prs:      client.PullRequests,
		comments: client.Issues,
		owner:    input.Owner,
		repo:     input.Repo,
		prNumber: input.PRNumber,
	}, nil
}

func (c *connector) writeReviewComment(block *github.PullRequestComment, commentId *int64, isRetry ...bool) error {
	ctx := context.Background()

	if commentId != nil {
		var _, err = c.prs.DeleteComment(ctx, c.owner, c.repo, *commentId)
		if err != nil {
			return err
		}
	}
	var _, resp, err = c.prs.CreateComment(ctx, c.owner, c.repo, c.prNumber, block)
	if err != nil {
		if resp != nil && resp.StatusCode == 422 {
			if len(isRetry) == 0 {
				time.Sleep(1 * time.Second)
				c.writeReviewComment(block, nil, true)
			} else {
				return newAbuseRateLimitError(c.owner, c.repo, c.prNumber, 1)
			}

		}
		return err
	}
	return nil
}

func (c *connector) writeGeneralComment(comment *github.IssueComment, isRetry ...bool) error {
	ctx := context.Background()

	var _, resp, err = c.comments.CreateComment(ctx, c.owner, c.repo, c.prNumber, comment)
	if err != nil {
		if resp != nil && resp.StatusCode == 422 {
			if len(isRetry) == 0 {
				time.Sleep(1 * time.Second)
				c.writeGeneralComment(comment, true)
			}
			return newAbuseRateLimitError(c.owner, c.repo, c.prNumber, 1)
		}
		return err
	}

	return nil
}

func (c *connector) getFilesForPr() ([]*github.CommitFile, error) {
	files, _, err := c.prs.ListFiles(context.Background(), c.owner, c.repo, c.prNumber, nil)
	if err != nil {
		return nil, err
	}
	var commitFiles []*github.CommitFile
	for _, file := range files {
		if *file.Status != "deleted" {
			commitFiles = append(commitFiles, file)
		}
	}
	return commitFiles, nil
}

func (c *connector) getExistingComments() ([]*existingComment, error) {
	ctx := context.Background()

	comments, _, err := c.prs.ListComments(ctx, c.owner, c.repo, c.prNumber, &github.PullRequestListCommentsOptions{})
	if err != nil {
		return nil, err
	}

	var existingComments []*existingComment
	for _, comment := range comments {
		existingComments = append(existingComments, &existingComment{
			filename:  comment.Path,
			comment:   comment.Body,
			commentId: comment.ID,
		})
	}
	return existingComments, nil
}

func (c *connector) prExists() bool {
	ctx := context.Background()

	_, _, err := c.prs.Get(ctx, c.owner, c.repo, c.prNumber)
	return err == nil
}
