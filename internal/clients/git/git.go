package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	maromdanaiov1alpha1 "my.domain/githubissue/api/v1alpha1"
	httpClient "my.domain/githubissue/internal/clients/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	APIBaseURL    = "https://api.github.com"
	open          = "open"
	closed        = "closed"
	url           = "%s/repos/%s/%s/issues"
	urlWithNumber = "%s/repos/%s/%s/issues/%d"
	secretName    = "github-token"
	secretKey     = "token"
	namespace     = "github-operator-system"
)

type GitClient interface {
	GetRepositoryIssues(owner string, repo string, logger logr.Logger) ([]maromdanaiov1alpha1.IssueResponse, error)
	CreateIssue(ctx context.Context, owner string, repo string, title string, body string, logger logr.Logger) (*maromdanaiov1alpha1.IssueResponse, error)
	UpdateIssue(ctx context.Context, owner string, repo string, number int, body string, title string, logger logr.Logger) (*maromdanaiov1alpha1.IssueResponse, error)
	CloseIssue(ctx context.Context, owner string, repo string, githubIssue *maromdanaiov1alpha1.GitHubIssue, logger logr.Logger) error
	FindIssue(issues []maromdanaiov1alpha1.IssueResponse, title string) *maromdanaiov1alpha1.IssueResponse
}

type GitClientInitializer interface {
	InitializeGit(ctx context.Context) (*http.Client, error)
}

type GitHubClientInitializer struct {
	HttpClient client.Client
}

// InitializeGit initialized an authorized client
func (g *GitHubClientInitializer) InitializeGit(ctx context.Context) (GitClient, error) {
	secret := &corev1.Secret{}
	err := g.HttpClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)
	if err != nil {
		return nil, errors.New("unable to read GitHub token secret: " + err.Error())
	}

	token, ok := secret.Data[secretKey]
	if !ok {
		return nil, errors.New("GitHub token not found in secret")
	}

	sourceToken := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: string(token)},
	)
	oauth2Client := oauth2.NewClient(ctx, sourceToken)
	HttpClient := &httpClient.HttpClient{
		Client: oauth2Client,
	}

	return &GitHubClient{HttpClient: HttpClient}, nil
}

type GitHubClient struct {
	HttpClient *httpClient.HttpClient
}

// GetRepositoryIssues gets all the issues listed in the given repository.
func (r *GitHubClient) GetRepositoryIssues(owner string, repo string, logger logr.Logger) ([]maromdanaiov1alpha1.IssueResponse, error) {
	url := fmt.Sprintf(url, APIBaseURL, owner, repo)

	issue := maromdanaiov1alpha1.IssueRequest{}

	response, err := r.HttpClient.SendRequest(url, http.MethodGet, issue)
	if err != nil {
		logger.Error(err, "failed to list all github issues")
		return nil, err
	}

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

// CreateIssue creates an issue.
func (r *GitHubClient) CreateIssue(ctx context.Context, owner string, repo string, title string, body string, logger logr.Logger) (*maromdanaiov1alpha1.IssueResponse, error) {
	url := createUrl(owner, repo)

	issue := maromdanaiov1alpha1.IssueRequest{
		Title: title,
		Body:  body,
		State: "open",
	}

	response, err := r.HttpClient.SendRequest(url, http.MethodPost, issue)

	if err != nil {
		logger.Error(err, "Failed to send request with status code", response.StatusCode)
		return nil, err
	}

	var result maromdanaiov1alpha1.IssueResponse
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// CloseIssue changes the issue status to "closed".
func (r *GitHubClient) CloseIssue(ctx context.Context, owner string, repo string, githubIssue *maromdanaiov1alpha1.GitHubIssue, logger logr.Logger) error {
	issues, err := r.GetRepositoryIssues(owner, repo, logger)
	if err != nil {
		return err
	}

	foundIssue := r.FindIssue(issues, githubIssue.Spec.Title)

	if foundIssue == nil {
		return fmt.Errorf("issue not found")
	}

	url := createUrlWithIssueNumber(owner, repo, foundIssue.Number)
	issue := &maromdanaiov1alpha1.IssueRequest{
		Title: githubIssue.Spec.Title,
		State: closed,
		Body:  githubIssue.Spec.Description,
	}

	response, err := r.HttpClient.SendRequest(url, http.MethodPost, issue)

	var result maromdanaiov1alpha1.IssueResponse
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		logger.Error(err, "Failed to send request")
		return err
	}
	return nil
}

// UpdateIssue updates the issue description.
func (r *GitHubClient) UpdateIssue(ctx context.Context, owner string, repo string, number int, body string, title string, logger logr.Logger) (*maromdanaiov1alpha1.IssueResponse, error) {
	url := createUrlWithIssueNumber(owner, repo, number)

	issue := &maromdanaiov1alpha1.IssueRequest{
		Title: title,
		Body:  body,
		State: open,
	}

	response, err := r.HttpClient.SendRequest(url, http.MethodPost, issue)

	var result maromdanaiov1alpha1.IssueResponse
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// FindIssue finds the issue in the lissues list with the same title as the one in thr githubIssue.
func (r *GitHubClient) FindIssue(issues []maromdanaiov1alpha1.IssueResponse, title string) *maromdanaiov1alpha1.IssueResponse {
	for _, issue := range issues {
		if issue.Title == title {
			return &issue
		}
	}
	return nil
}

// createUrl returns the gitHub url we need to send / get the request to / from.
func createUrl(owner string, repo string) string {
	return fmt.Sprintf(url, APIBaseURL, owner, repo)
}

// createUrlWithIssueNumber returns the gitHub url we need to send / get the request to / from with a specific issue number.
func createUrlWithIssueNumber(owner string, repo string, number int) string {
	return fmt.Sprintf(urlWithNumber, APIBaseURL, owner, repo, number)
}
