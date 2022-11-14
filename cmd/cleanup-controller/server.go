package main

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
)

// DebugModeOptions holds the options to configure debug mode
type DebugModeOptions struct {
	// DumpPayload is used to activate/deactivate debug mode.
	DumpPayload bool
}

type Server interface {
	// Run TLS server in separate thread and returns control immediately
	Run(<-chan struct{})
	// Stop TLS server and returns control after the server is shut down
	Stop(context.Context)
	// Cleanup returns the chanel used to wait for the server to clean up resources
	Cleanup() <-chan struct{}
}

type CleanupPolicyHandlers interface {
	// Validate performs the validation check on policy resources
	Validate(logr.Logger, *admissionv1.AdmissionRequest, time.Time) *admissionv1.AdmissionResponse
}
