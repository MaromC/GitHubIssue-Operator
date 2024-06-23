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
	"github.com/migueleliasweb/go-github-mock/src/mock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/oauth2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	maromdanaiov1alpha1 "my.domain/githubissue/api/v1alpha1"
	"net/http"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("GitHubIssue Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		githubissue := &maromdanaiov1alpha1.GitHubIssue{}

		setUpMockedClient := func(issues []maromdanaiov1alpha1.IssueResponse, url string, number int) func(ctx context.Context) (*http.Client, error) {
			mockedHTTPClient := mock.NewMockedHTTPClient(
				mock.WithRequestMatchHandler(
					mock.GetReposIssuesByOwnerByRepo,
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						_ = json.NewEncoder(w).Encode(issues)
					}),
				),
				mock.WithRequestMatchHandler(
					mock.PostReposIssuesByOwnerByRepo,
					http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var issueReq maromdanaiov1alpha1.IssueRequest
						_ = json.NewDecoder(r.Body).Decode(&issueReq)
						issueResp := &maromdanaiov1alpha1.IssueResponse{
							URL:    url,
							Number: number,
							Title:  issueReq.Title,
							Body:   issueReq.Body,
							State:  "open",
						}
						_ = json.NewEncoder(w).Encode(issueResp)
					}),
				),
			)

			return func(ctx context.Context) (*http.Client, error) {
				token := os.Getenv("GITHUB_TOKEN")
				if token == "" {
					return nil, errors.New("GitHub token is not set")
				}
				sourceToken := oauth2.StaticTokenSource(
					&oauth2.Token{AccessToken: token},
				)
				client := oauth2.NewClient(ctx, sourceToken)
				client.Transport = mockedHTTPClient.Transport
				return client, nil
			}
		}

		reconcileGitHubIssue := func(getClient func(ctx context.Context) (*http.Client, error)) error {
			controllerReconciler := &GitHubIssueReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				GetClient: getClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			return err
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind GitHubIssue")
			err := k8sClient.Get(ctx, typeNamespacedName, githubissue)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &maromdanaiov1alpha1.GitHubIssue{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: maromdanaiov1alpha1.GitHubIssueSpec{
						Repo:        "MaromC/GitHubIssue-Operator",
						Title:       "Test Issue",
						Description: "This is a test issue",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {

			resource := &maromdanaiov1alpha1.GitHubIssue{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GitHubIssue")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		// Test #1
		It("should successfully reconcile the resource", func() {

			By("Setting up the mocked GitHub client")
			issues := []maromdanaiov1alpha1.IssueResponse{}
			getClient := setUpMockedClient(issues, "https://api.github.com/repos/owner/repo/issues/1", 1)

			By("Reconciling the created resource")
			err := reconcileGitHubIssue(getClient)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the issue was created in GitHub with the right conditions")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, typeNamespacedName, githubissue); err != nil {
					return false
				}
				return len(githubissue.Status.Conditions) > 0
			}).Should(BeTrue())

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, typeNamespacedName, githubissue); err != nil {
					return false
				}
				for _, condition := range githubissue.Status.Conditions {
					if condition.Type == "OpenIssue" && condition.Status == metav1.ConditionTrue {
						return true
					}
				}
				return false
			}).Should(BeTrue())
		})

		// Test #2
		It("should update the issue if it already exists", func() {

			By("Setting up the mocked GitHub client")
			issues := []maromdanaiov1alpha1.IssueResponse{
				{
					URL:    "https://api.github.com/repos/owner/repo/issues/1",
					Number: 1,
					Title:  "Test Issue",
					Body:   "Old description",
					State:  "open",
				},
			}
			getClient := setUpMockedClient(issues, "https://api.github.com/repos/owner/repo/issues/1", 1)

			By("Reconciling the created resource")
			controllerReconciler := &GitHubIssueReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				GetClient: getClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the issue description was updated")
			Eventually(func() string {
				if err := k8sClient.Get(ctx, typeNamespacedName, githubissue); err != nil {
					return ""
				}
				return githubissue.Spec.Description
			}).Should(Equal("This is a test issue"))
		})

		// Test #3
		It("should handle missing GitHub token", func() {
			By("Setting up the mocked GitHub client with no token")

			getClient := func(ctx context.Context) (*http.Client, error) {
				return nil, errors.New("GitHub token is not set")
			}

			By("Reconciling the created resource")
			controllerReconciler := &GitHubIssueReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				GetClient: getClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})

			By("Verifying the correct error is returned")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("GitHub token is not set"))
		})

		// Test #4
		It("should handle deletion of the GitHubIssue resource", func() {

			By("Setting up the mocked GitHub client")
			issues := []maromdanaiov1alpha1.IssueResponse{}
			getClient := setUpMockedClient(issues, "https://api.github.com/repos/owner/repo/issues/2", 2)

			By("Reconciling the created resource")
			controllerReconciler := &GitHubIssueReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				GetClient: getClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Deleting the GitHubIssue resource")
			Expect(k8sClient.Delete(ctx, githubissue)).To(Succeed())

			By("Verifying the GitHubIssue resource is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, githubissue)
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())

		})
		// Test #5
		It("should handle a failed attempt to create a real GitHub issue", func() {

			By("Setting up the mocked GitHub client to return a failure for post")
			getClient := func(ctx context.Context) (*http.Client, error) {
				mockedHTTPClient := mock.NewMockedHTTPClient(
					mock.WithRequestMatchHandler(
						mock.PostReposIssuesByOwnerByRepo,
						http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							http.Error(w, "Failed to create issue", http.StatusInternalServerError)
						}),
					),
				)
				return &http.Client{Transport: mockedHTTPClient.Transport}, nil
			}

			By("Reconciling the created resource")
			controllerReconciler := &GitHubIssueReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				GetClient: getClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})

			By("Verifying the correct error is returned")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Failed to create issue"))
		})

		// Test #6
		It("should handle a failed attempt to update a GitHub issue", func() {

			By("Setting up the mocked GitHub client to return a failure for post")
			issues := []maromdanaiov1alpha1.IssueResponse{
				{
					URL:    "https://api.github.com/repos/owner/repo/issues/1",
					Number: 1,
					Title:  "Test Issue",
					Body:   "Old description",
					State:  "open",
				},
			}
			getClient := func(ctx context.Context) (*http.Client, error) {
				mockedHTTPClient := mock.NewMockedHTTPClient(
					mock.WithRequestMatchHandler(
						mock.GetReposIssuesByOwnerByRepo,
						http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							_ = json.NewEncoder(w).Encode(issues)
						}),
					),
					mock.WithRequestMatchHandler(
						mock.PostReposIssuesByOwnerByRepo,
						http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							http.Error(w, "Failed to update issue", http.StatusInternalServerError)
						}),
					),
				)
				return &http.Client{Transport: mockedHTTPClient.Transport}, nil
			}

			By("Reconciling the created resource")
			controllerReconciler := &GitHubIssueReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				GetClient: getClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})

			By("Verifying the correct error is returned")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Failed to update issue"))
		})

	})
})
