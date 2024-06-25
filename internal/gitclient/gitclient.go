package gitclient

import (
	"context"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	maromdanaiov1alpha1 "my.domain/githubissue/api/v1alpha1"
)

type GitClient interface {
	GetRepositoryIssues(ctx context.Context, owner string, repo string, logger logr.Logger, client client.Client, request ctrl.Request) ([]maromdanaiov1alpha1.IssueResponse, error)
	CreateIssue(ctx context.Context, owner string, repo string, title string, body string, logger logr.Logger, client client.Client, request ctrl.Request) (*maromdanaiov1alpha1.IssueResponse, error)
	UpdateIssue(ctx context.Context, owner string, repo string, number int, body string, title string, logger logr.Logger, client client.Client, request ctrl.Request) (*maromdanaiov1alpha1.IssueResponse, error)
	CloseIssue(ctx context.Context, owner string, repo string, githubIssue *maromdanaiov1alpha1.GitHubIssue, logger logr.Logger, client client.Client, request ctrl.Request) error
	FindIssue(issues []maromdanaiov1alpha1.IssueResponse, title string) *maromdanaiov1alpha1.IssueResponse
}
