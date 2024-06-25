/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	maromdanaiov1alpha1 "my.domain/githubissue/api/v1alpha1"
	"net/http"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strings"
	"time"
)

const (
	APIBaseURL = "https://api.github.com"
	finalizer  = "githubIssue.finalizers.my.domain"
	EmptyOther = -1
)

// GitHubIssueReconciler reconciles a GitHubIssue object
type GitHubIssueReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Logger    logr.Logger
	GetClient func(ctx context.Context) (*http.Client, error)
}

//+kubebuilder:rbac:groups=marom.dana.io.dana.io,resources=githubissues,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=marom.dana.io.dana.io,resources=githubissues/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=marom.dana.io.dana.io,resources=githubissues/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *GitHubIssueReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("Namespace", req.Namespace, "name", req.Name)
	githubIssue := &maromdanaiov1alpha1.GitHubIssue{}
	if err := r.Get(ctx, req.NamespacedName, githubIssue); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to fetch GitHubIssue")
			return ctrl.Result{}, err
		}
		logger.Error(err, "GitHubIssue resource Not found")
		return ctrl.Result{}, nil
	}

	owner, repo := GetOwnerAndRepo(*githubIssue)

	if err := r.CheckDeletion(ctx, githubIssue, owner, repo); err != nil {
		if errors.Is(errors.Unwrap(err), errors.New("NamespaceLabel CR deletion has been handled")) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	issues, err := r.GetRepositoryIssues(ctx, owner, repo)
	if err != nil {
		r.Logger.Error(err, "Failed to list all repository issues")
		return ctrl.Result{}, err
	}

	foundIssue := FindIssue(issues, githubIssue)

	handledIssue, err := r.HandleIssues(foundIssue, ctx, owner, repo, githubIssue)
	if err != nil {
		r.Logger.Error(err, "Failed to create/update issue")
		return ctrl.Result{}, err
	}

	conditions := CreateConditions(handledIssue)

	githubIssue.Status.Conditions = conditions
	if err = r.Status().Update(ctx, githubIssue); err != nil {
		r.Logger.Error(err, "Failed to update GitHubIssue status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// GetRepositoryIssues gets all the issues listed in the given repository
func (r *GitHubIssueReconciler) GetRepositoryIssues(ctx context.Context, owner string, repo string) ([]maromdanaiov1alpha1.IssueResponse, error) {
	url := CreateUrl(owner, repo, EmptyOther)

	githubClient, err := r.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
		r.Logger.Error(err, "")
		return nil, err
	}

	var issues []maromdanaiov1alpha1.IssueResponse
	if err := json.NewDecoder(response.Body).Decode(&issues); err != nil {
		return nil, err
	}

	return issues, nil
}

// CreateIssue creates an issue
func (r *GitHubIssueReconciler) CreateIssue(ctx context.Context, owner string, repo string, title string, body string) (*maromdanaiov1alpha1.IssueResponse, error) {
	url := CreateUrl(owner, repo, EmptyOther)

	issue := maromdanaiov1alpha1.IssueRequest{
		Title: title,
		Body:  body,
		State: "open",
	}

	return r.SendRequest(ctx, url, "POST", issue)
}

// SendRequest sends a request to github
func (r *GitHubIssueReconciler) SendRequest(ctx context.Context, url string, method string, body interface{}) (*maromdanaiov1alpha1.IssueResponse, error) {
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
func (r *GitHubIssueReconciler) UpdateIssue(ctx context.Context, owner string, repo string, number int, body string, title string) (*maromdanaiov1alpha1.IssueResponse, error) {
	url := CreateUrl(owner, repo, number)

	issue := &maromdanaiov1alpha1.IssueRequest{
		Title: title,
		Body:  body,
		State: "open",
	}

	return r.SendRequest(ctx, url, "POST", issue)
}

// CloseIssue changes the issue status to "closed"
func (r *GitHubIssueReconciler) CloseIssue(ctx context.Context, owner string, repo string, githubIssue *maromdanaiov1alpha1.GitHubIssue) error {
	issues, err := r.GetRepositoryIssues(ctx, owner, repo)
	if err != nil {
		return err
	}

	foundIssue := FindIssue(issues, githubIssue)

	if foundIssue == nil {
		return fmt.Errorf("issue not found")
	}

	url := CreateUrl(owner, repo, foundIssue.Number)
	issue := &maromdanaiov1alpha1.IssueRequest{
		Title: githubIssue.Spec.Title,
		State: "closed",
		Body:  githubIssue.Spec.Description,
	}

	_, err = r.SendRequest(ctx, url, "POST", issue)
	if err != nil {
		r.Logger.Error(err, "Failed to send request")
		return err
	}
	return nil
}

// HandleIssues creates an issue with the needed data if it dosent exist, if it does, it updated the existing issue
func (r *GitHubIssueReconciler) HandleIssues(foundIssue *maromdanaiov1alpha1.IssueResponse, ctx context.Context, owner string, repo string, githubIssue *maromdanaiov1alpha1.GitHubIssue) (*maromdanaiov1alpha1.IssueResponse, error) {
	if foundIssue == nil {
		newIssue, err := r.CreateIssue(ctx, owner, repo, githubIssue.Spec.Title, githubIssue.Spec.Description)
		if err != nil {
			r.Logger.Error(err, "Failed to create issue")
			return nil, err
		}
		return newIssue, nil
	}
	if foundIssue.Body != githubIssue.Spec.Description {
		updatedIssue, err := r.UpdateIssue(ctx, owner, repo, foundIssue.Number, githubIssue.Spec.Description, githubIssue.Spec.Title)
		if err != nil {
			r.Logger.Error(err, "Failed to update issue")
			return nil, err
		}
		return updatedIssue, nil
	}
	return nil, nil
}

// CreateConditions create the conditions for the issue
func CreateConditions(issue *maromdanaiov1alpha1.IssueResponse) []metav1.Condition {
	conditions := []metav1.Condition{
		{
			Type:               "OpenIssue",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "IssueExists",
			Message:            "Issue is open",
		},
	}
	if issue.PullRequestLinks != nil {
		conditions = append(conditions, metav1.Condition{
			Type:               "IssueHasPR",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "IssueHasAPRLink",
			Message:            "Issue has a PR",
		})
		return conditions
	}
	conditions = append(conditions, metav1.Condition{
		Type:               "IssueHasPR",
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "IssueHasNoNoPR",
		Message:            "Issue does not have a PR",
	})
	return conditions
}

// CheckDeletion checks if the NamespaceLabel CRD has been deleted
func (r *GitHubIssueReconciler) CheckDeletion(ctx context.Context, githubIssue *maromdanaiov1alpha1.GitHubIssue, owner string, repo string) error {
	if !githubIssue.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(githubIssue, finalizer) {
			if err := r.CloseIssue(ctx, owner, repo, githubIssue); err != nil {
				return err
			}
			controllerutil.RemoveFinalizer(githubIssue, finalizer)

			if err := r.Update(ctx, githubIssue); err != nil {
				return err
			}
			return errors.New("GitHubIssue CR deletion has been handeld")
		}
		return errors.New("GitHubIssue CR may have been deleted")
	}

	if controllerutil.AddFinalizer(githubIssue, finalizer) {
		if err := r.Update(ctx, githubIssue); err != nil {
			return err
		}
	}

	return nil
}

// CreateUrl returns the gitHub url we need to send / get the request to / from
func CreateUrl(owner string, repo string, other int) string {
	if other != -1 {
		return fmt.Sprintf("%s/repos/%s/%s/issues/%d", APIBaseURL, owner, repo, other)

	}
	return fmt.Sprintf("%s/repos/%s/%s/issues", APIBaseURL, owner, repo)
}

// GetOwnerAndRepo returns the owner and repo parts from the githubIssue repo string
func GetOwnerAndRepo(githubIssue maromdanaiov1alpha1.GitHubIssue) (string, string) {
	repoParts := strings.Split(githubIssue.Spec.Repo, "/")
	owner := repoParts[len(repoParts)-2]
	repo := repoParts[len(repoParts)-1]
	return owner, repo
}

// FindIssue finds the issue in the lissues list with the same title as the one in thr githubIssue
func FindIssue(issues []maromdanaiov1alpha1.IssueResponse, githubIssue *maromdanaiov1alpha1.GitHubIssue) *maromdanaiov1alpha1.IssueResponse {
	for _, issue := range issues {
		if issue.Title == githubIssue.Spec.Title {
			return &issue
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitHubIssueReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&maromdanaiov1alpha1.GitHubIssue{}).
		Complete(r)
}
