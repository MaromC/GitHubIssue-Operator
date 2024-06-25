package http

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-logr/logr"
	maromdanaiov1alpha1 "my.domain/githubissue/api/v1alpha1"
	"net/http"
	"strings"
)

var (
	APIBaseURL = "https://api.github.com"
)

type GitHubClient struct {
	GetClient func(ctx context.Context) (*http.Client, error)
}

// GetRepositoryIssues gets all the issues listed in the given repository
func (r *GitHubClient) GetRepositoryIssues(ctx context.Context, owner string, repo string, logger logr.Logger) ([]maromdanaiov1alpha1.IssueResponse, error) {
	url := CreateUrl(owner, repo)

	githubClient, err := r.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	response, err := githubClient.Do(request)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("failed to list all github issues with status code: %d", response.StatusCode)
		logger.Error(err, "")
		return nil, err
	}

	var issues []maromdanaiov1alpha1.IssueResponse
	if err := json.NewDecoder(response.Body).Decode(&issues); err != nil {
		return nil, err
	}

	return issues, nil
}

// CreateIssue creates an issue
func (r *GitHubClient) CreateIssue(ctx context.Context, owner string, repo string, title string, body string) (*maromdanaiov1alpha1.IssueResponse, error) {
	url := CreateUrl(owner, repo)

	issue := maromdanaiov1alpha1.IssueRequest{
		Title: title,
		Body:  body,
		State: "open",
	}

	return r.SendRequest(ctx, url, http.MethodPost, issue)
}

// SendRequest sends a request to github
func (r *GitHubClient) SendRequest(ctx context.Context, url string, method string, body interface{}) (*maromdanaiov1alpha1.IssueResponse, error) {
	githubClient, err := r.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	requestBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, strings.NewReader(string(requestBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	response, err := githubClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	var result maromdanaiov1alpha1.IssueResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// UpdateIssue updates the issue description
func (r *GitHubClient) UpdateIssue(ctx context.Context, owner string, repo string, number int, body string, title string) (*maromdanaiov1alpha1.IssueResponse, error) {
	url := CreateUrlWithIssueNumber(owner, repo, number)

	issue := &maromdanaiov1alpha1.IssueRequest{
		Title: title,
		Body:  body,
		State: "open",
	}

	return r.SendRequest(ctx, url, http.MethodPost, issue)
}

// CloseIssue changes the issue status to "closed"
func (r *GitHubClient) CloseIssue(ctx context.Context, owner string, repo string, githubIssue *maromdanaiov1alpha1.GitHubIssue, logger logr.Logger) error {
	issues, err := r.GetRepositoryIssues(ctx, owner, repo, logger)
	if err != nil {
		return err
	}

	foundIssue := r.FindIssue(issues, githubIssue.Spec.Title)

	if foundIssue == nil {
		return fmt.Errorf("issue not found")
	}

	url := CreateUrlWithIssueNumber(owner, repo, foundIssue.Number)
	issue := &maromdanaiov1alpha1.IssueRequest{
		Title: githubIssue.Spec.Title,
		State: "closed",
		Body:  githubIssue.Spec.Description,
	}

	_, err = r.SendRequest(ctx, url, http.MethodPost, issue)
	if err != nil {
		logger.Error(err, "Failed to send request")
		return err
	}
	return nil
}

// CreateUrl returns the gitHub url we need to send / get the request to / from
func CreateUrl(owner string, repo string) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/", APIBaseURL, owner, repo)
}

// CreateUrlWithIssueNumber returns the gitHub url we need to send / get the request to / from with a specific issue number
func CreateUrlWithIssueNumber(owner string, repo string, number int) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d", APIBaseURL, owner, repo, number)
}

// FindIssue finds the issue in the lissues list with the same title as the one in thr githubIssue
func (r *GitHubClient) FindIssue(issues []maromdanaiov1alpha1.IssueResponse, title string) *maromdanaiov1alpha1.IssueResponse {
	for _, issue := range issues {
		if issue.Title == title {
			return &issue
		}
	}
	return nil
}
