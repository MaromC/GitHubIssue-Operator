package webhook

import (
	"context"
	"encoding/json"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"net/http"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type NamespaceValidator struct {
	Client  client.Client
	Decoder *admission.Decoder
}

type LogInfo struct {
	User      string       `json:"user"`
	Operation v1.Operation `json:"operation"`
}

func (wh *NamespaceValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	namespace := &corev1.Namespace{}

	if err := wh.Decoder.Decode(req, namespace); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	response := wh.handle(req, namespace)
	return response

}

func (wh *NamespaceValidator) handle(request admission.Request, namespace *corev1.Namespace) admission.Response {
	username := request.UserInfo.Username
	operation := request.Operation

	logEntry := &LogInfo{
		User:      username,
		Operation: operation,
	}

	logData, err := json.Marshal(logEntry)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	logFile, err := os.OpenFile("/var/log/webhook.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	defer logFile.Close()

	if _, err := logFile.WriteString(string(logData) + "\n"); err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.Allowed("Username written o")
}
