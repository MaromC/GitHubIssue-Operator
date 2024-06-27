package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	"my.domain/githubissue/internal/gitclient"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"

	"github.com/go-logr/logr"
	maromdanaiov1alpha1 "my.domain/githubissue/api/v1alpha1"
)

var (
	APIBaseURL       = "https://api.github.com"
	accept           = "Accept"
	acceptValue      = "application/vnd.github.v3+json"
	contentType      = "Content-Type"
	contentTypeValue = "application/json"
	open             = "open"
	closed           = "closed"
	url              = "%s/repos/%s/%s/issues/"
	urlWithNumber    = "%s/repos/%s/%s/issues/%d"
	secretName       = "github-token"
	secretKey        = "token"
	namespace        = "github-operator-system"
)

type GitHubClient struct {
	K8sClient *http.Client
}

type GitHubClientInitializer struct {
	K8sClient client.Client
}

// InitializeGit initialized an authorized client
func (g *GitHubClientInitializer) InitializeGit(ctx context.Context) (gitclient.GitClient, error) {
	secret := &corev1.Secret{}
	err := g.K8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)
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
	httpClient := oauth2.NewClient(ctx, sourceToken)

	return &GitHubClient{K8sClient: httpClient}, nil
}

// GetRepositoryIssues gets all the issues listed in the given repository.
func (r *GitHubClient) GetRepositoryIssues(ctx context.Context, owner string, repo string, logger logr.Logger) ([]maromdanaiov1alpha1.IssueResponse, error) {
	url := createUrl(owner, repo)
	//
	//githubClient, err := r.GetClient(ctx, r.K8sClient)
	//if err != nil {
	//	return nil, err
	//}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	response, err := r.K8sClient.Do(request)
	if err != nil {
		return nil, err
	}

	defer func() {
		err := response.Body.Close()
		if err != nil {
			logger.Error(err, "failed closing response body")
		}
	}()

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

	return r.SendRequest(ctx, url, http.MethodPost, issue, logger)
}

// SendRequest sends a request to github.
func (r *GitHubClient) SendRequest(ctx context.Context, url string, method string, body interface{}, logger logr.Logger) (*maromdanaiov1alpha1.IssueResponse, error) {
	//githubClient, err := r.GetClient(ctx, r.K8sClient)
	//if err != nil {
	//	return nil, err
	//}

	requestBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, strings.NewReader(string(requestBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set(accept, acceptValue)
	req.Header.Set(contentType, contentTypeValue)

	response, err := r.K8sClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		err := response.Body.Close()
		if err != nil {
			logger.Error(err, "failed closing response body")
		}
	}()

	var result maromdanaiov1alpha1.IssueResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// UpdateIssue updates the issue description.
func (r *GitHubClient) UpdateIssue(ctx context.Context, owner string, repo string, number int, body string, title string, logger logr.Logger) (*maromdanaiov1alpha1.IssueResponse, error) {
	url := createUrlWithIssueNumber(owner, repo, number)

	issue := &maromdanaiov1alpha1.IssueRequest{
		Title: title,
		Body:  body,
		State: open,
	}

	return r.SendRequest(ctx, url, http.MethodPost, issue, logger)
}

// CloseIssue changes the issue status to "closed".
func (r *GitHubClient) CloseIssue(ctx context.Context, owner string, repo string, githubIssue *maromdanaiov1alpha1.GitHubIssue, logger logr.Logger) error {
	issues, err := r.GetRepositoryIssues(ctx, owner, repo, logger)
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

	_, err = r.SendRequest(ctx, url, http.MethodPost, issue, logger)
	if err != nil {
		logger.Error(err, "Failed to send request")
		return err
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

// FindIssue finds the issue in the lissues list with the same title as the one in thr githubIssue.
func (r *GitHubClient) FindIssue(issues []maromdanaiov1alpha1.IssueResponse, title string) *maromdanaiov1alpha1.IssueResponse {
	for _, issue := range issues {
		if issue.Title == title {
			return &issue
		}
	}
	return nil
}
