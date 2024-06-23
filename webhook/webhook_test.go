package webhook

import (
	"context"
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	maromdanaiov1alpha1 "my.domain/githubissue/api/v1alpha1"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ = Describe("Namespace Webhook", func() {
	var (
		namespaceValidator *NamespaceValidator
	)

	CheckUserInFile := func(username string) {
		logData, err := os.ReadFile("/var/log/webhook.log")
		Expect(err).NotTo(HaveOccurred())

		var logEntry LogInfo
		err = json.Unmarshal(logData, &logEntry)
		Expect(err).NotTo(HaveOccurred())
		Expect(logEntry.User).To(Equal(username))
	}

	CreateAdmissionRequest := func(username string, operation v1.Operation) admission.Request {
		request := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				UserInfo:  authenticationv1.UserInfo{Username: username},
				Operation: operation,
			},
		}
		return request
	}

	HandleRequest := func(request admission.Request, namespace corev1.Namespace) {
		response := namespaceValidator.handle(request, &namespace)
		Expect(response.Allowed).To(BeTrue())
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}

	BeforeEach(func() {
		ctx = context.Background()
		scheme := runtime.NewScheme()
		Expect(maromdanaiov1alpha1.AddToScheme(scheme)).To(Succeed())
		err := corev1.AddToScheme(scheme)
		Expect(err).NotTo(HaveOccurred())
		namespaceValidator = &NamespaceValidator{
			Client:  fake.NewClientBuilder().WithScheme(scheme).Build(),
			Decoder: nil,
		}
		err = admission.NewDecoder(scheme)
		Expect(err).ToNot(HaveOccurred())

		err = os.WriteFile("/var/log/webhook.log", []byte{}, 0644)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should log the username to the file when creating a namespace", func() {

		request := CreateAdmissionRequest("create-user", admissionv1.Create)

		HandleRequest(request, *namespace)

		CheckUserInFile("create-user")
	})

	It("should log the username to the file when deleting a namespace", func() {

		request := CreateAdmissionRequest("delete-user", admissionv1.Delete)

		HandleRequest(request, *namespace)

		CheckUserInFile("delete-user")

	})

	It("should log the username to the file when updating a namespace", func() {

		request := CreateAdmissionRequest("update-user", admissionv1.Update)

		HandleRequest(request, *namespace)

		CheckUserInFile("update-user")
	})
})
