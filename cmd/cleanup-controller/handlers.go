package main

import (
	"time"

	"github.com/go-logr/logr"
	"github.com/kyverno/kyverno/cmd/cleanup-controller/utils"
	"github.com/kyverno/kyverno/cmd/cleanup-controller/validate"
	"github.com/kyverno/kyverno/pkg/clients/dclient"
	"github.com/kyverno/kyverno/pkg/openapi"
	admissionutils "github.com/kyverno/kyverno/pkg/utils/admission"
	admissionv1 "k8s.io/api/admission/v1"
)

type cleanupPolicyHandlers struct {
	client         dclient.Interface
	openApiManager openapi.Manager
}

func NewHandlers(client dclient.Interface, openApiManager openapi.Manager) CleanupPolicyHandlers {
	return &cleanupPolicyHandlers{
		client:         client,
		openApiManager: openApiManager,
	}
}

func (h *cleanupPolicyHandlers) Validate(logger logr.Logger, request *admissionv1.AdmissionRequest, _ time.Time) *admissionv1.AdmissionResponse {
	if request.SubResource != "" {
		logger.V(4).Info("skip policy validation on status update")
		return admissionutils.ResponseSuccess()
	}
	policy, _, err := utils.GetCleanupPolicies(request)
	if err != nil {
		logger.Error(err, "failed to unmarshal policies from admission request")
		return admissionutils.Response(err)
	}
	err = validate.ValidateCleanupPolicy(policy, h.client, false, h.openApiManager)
	if err != nil {
		logger.Error(err, "policy validation errors")
		return admissionutils.Response(err)
	}
	return admissionutils.Response(err)
}
