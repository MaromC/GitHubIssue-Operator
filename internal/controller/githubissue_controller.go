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
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	maromdanaiov1alpha1 "my.domain/githubissue/api/v1alpha1"
	"my.domain/githubissue/internal/gitclient"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	finalizer        = "githubIssue.finalizers.my.domain"
	issueHasPr       = "IssueHasPR"
	hasPrLink        = "IssueHasAPRLink"
	hasPrMessage     = "Issue has a PR"
	hasNoPr          = "IssueHasNoPR"
	hasNoPrMessage   = "Issue does not have a PR"
	openIssue        = "OpenIssue"
	issueExists      = "IssueExists"
	openIssueMessage = "Issue is open"
)

// GitHubIssueReconciler reconciles a GitHubIssue object
type GitHubIssueReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Logger    logr.Logger
	GitClient gitclient.GitClient
	GetClient func(ctx context.Context, k8sClient client.Client) (*http.Client, error)
}

//+kubebuilder:rbac:groups=marom.dana.io.dana.io,resources=githubissues,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=marom.dana.io.dana.io,resources=githubissues/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=marom.dana.io.dana.io,resources=githubissues/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *GitHubIssueReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("namespace", req.Namespace, "name", req.Name)
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

	if err := r.CheckDeletion(ctx, githubIssue, owner, repo, r.GitClient); err != nil {
		if errors.Is(errors.Unwrap(err), errors.New("NamespaceLabel CR deletion has been handled")) ||
			errors.Is(errors.Unwrap(err), errors.New("GitHubIssue CR may have been deleted")) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	issues, err := r.GitClient.GetRepositoryIssues(ctx, owner, repo, r.Logger)
	if err != nil {
		r.Logger.Error(err, "Failed to list all repository issues")
		return ctrl.Result{}, err
	}

	foundIssue := r.GitClient.FindIssue(issues, githubIssue.Spec.Title)

	handledIssue, err := r.HandleIssues(foundIssue, ctx, owner, repo, githubIssue, r.GitClient)
	if err != nil {
		r.Logger.Error(err, "Failed to create/update issue")
		return ctrl.Result{}, err
	}

	r.updateConditions(githubIssue, handledIssue)

	if err = r.Status().Update(ctx, githubIssue); err != nil {
		r.Logger.Error(err, "Failed to update GitHubIssue status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// HandleIssues creates an issue with the needed data if it dosent exist, if it does, it updated the existing issue.
func (r *GitHubIssueReconciler) HandleIssues(foundIssue *maromdanaiov1alpha1.IssueResponse, ctx context.Context, owner string, repo string, githubIssue *maromdanaiov1alpha1.GitHubIssue, gitClient gitclient.GitClient) (*maromdanaiov1alpha1.IssueResponse, error) {
	if foundIssue == nil {
		newIssue, err := gitClient.CreateIssue(ctx, owner, repo, githubIssue.Spec.Title, githubIssue.Spec.Description, r.Logger)
		if err != nil {
			r.Logger.Error(err, "Failed to create issue")
			return nil, err
		}
		return newIssue, nil
	}
	if foundIssue.Body != githubIssue.Spec.Description {
		updatedIssue, err := gitClient.UpdateIssue(ctx, owner, repo, foundIssue.Number, githubIssue.Spec.Description, githubIssue.Spec.Title, r.Logger)
		if err != nil {
			r.Logger.Error(err, "Failed to update issue")
			return nil, err
		}
		return updatedIssue, nil
	}
	return nil, nil
}

// updateConditions updates the conditions for the GitHubIssue.
func (r *GitHubIssueReconciler) updateConditions(githubIssue *maromdanaiov1alpha1.GitHubIssue, issue *maromdanaiov1alpha1.IssueResponse) {
	openCondition := metav1.Condition{
		Type:               openIssue,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             issueExists,
		Message:            openIssueMessage,
	}

	prCondition := metav1.Condition{
		Type:               issueHasPr,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             hasNoPr,
		Message:            hasNoPrMessage,
	}

	if issue != nil && issue.PullRequestLinks != nil {
		prCondition.Status = metav1.ConditionTrue
		prCondition.Reason = hasPrLink
		prCondition.Message = hasPrMessage
	}

	meta.SetStatusCondition(&githubIssue.Status.Conditions, openCondition)
	meta.SetStatusCondition(&githubIssue.Status.Conditions, prCondition)
}

// CheckDeletion checks if the GitHubIssue CRD has been deleted and if deleted handles it.
func (r *GitHubIssueReconciler) CheckDeletion(ctx context.Context, githubIssue *maromdanaiov1alpha1.GitHubIssue, owner string, repo string, gitClient gitclient.GitClient) error {
	if !githubIssue.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(githubIssue, finalizer) {
			if err := gitClient.CloseIssue(ctx, owner, repo, githubIssue, r.Logger); err != nil {
				return err
			}
			controllerutil.RemoveFinalizer(githubIssue, finalizer)

			if err := r.Update(ctx, githubIssue); err != nil {
				return err
			}
			return errors.New("GitHubIssue CR deletion has been handled")
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

// GetOwnerAndRepo returns the owner and repo parts from the githubIssue repo string.
func GetOwnerAndRepo(githubIssue maromdanaiov1alpha1.GitHubIssue) (string, string) {
	repoParts := strings.Split(githubIssue.Spec.Repo, "/")
	owner := repoParts[len(repoParts)-2]
	repo := repoParts[len(repoParts)-1]
	return owner, repo
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitHubIssueReconciler) SetupWithManager(mgr ctrl.Manager, syncPeriod time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&maromdanaiov1alpha1.GitHubIssue{}).
		Complete(r)
}
