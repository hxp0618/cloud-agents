package server

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type remoteWorkerEnrollmentStore interface {
	CreateRemoteWorkerEnrollment(context.Context, string, *authn.VerifiedPrincipal, internalremoteworker.CreateInput) (internalremoteworker.Snapshot, error)
	ClaimRemoteWorkerEnrollmentSecret(context.Context, string, *authn.VerifiedPrincipal, internalremoteworker.TransitionInput, string) (internalremoteworker.Snapshot, error)
	RevokeRemoteWorkerEnrollment(context.Context, string, *authn.VerifiedPrincipal, internalremoteworker.TransitionInput) (internalremoteworker.Snapshot, error)
	GetRemoteWorkerEnrollment(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalremoteworker.Snapshot, error)
	ListRemoteWorkerEnrollments(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.RemoteWorkerEnrollmentPage, error)
	ListRemoteWorkerEnrollmentAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.RemoteWorkerEnrollmentAuditPage, error)
	ListRemoteWorkerOperations(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.RemoteWorkerOperationPage, error)
	PreviewRemoteWorkerScheduling(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalremoteworker.SchedulingPreview, error)
	TransitionRemoteWorkerScheduling(context.Context, string, *authn.VerifiedPrincipal, internalremoteworker.SchedulingInput) (internalremoteworker.Operation, error)
	AuthenticateRemoteWorkerCertificate(context.Context, string, string, string, string, int64) error
	AuthorizeRemoteWorkerCertificateRotation(context.Context, string, internalremoteworker.CertificateRotationRequest) (postgres.RemoteWorkerCertificateRotationAuthorization, error)
	IssueRemoteWorkerCertificate(context.Context, string, internalremoteworker.CertificatePersistenceInput) (internalremoteworker.Snapshot, error)
	RotateRemoteWorkerCertificate(context.Context, string, internalremoteworker.CertificateRotationPersistenceInput) (internalremoteworker.Snapshot, error)
	HeartbeatRemoteWorker(context.Context, string, internalremoteworker.HeartbeatInput) (postgres.RemoteWorkerHeartbeatResult, error)
}

type RemoteWorkerEnrollmentHTTPServer struct {
	verifier  AccessTokenVerifier
	store     remoteWorkerEnrollmentStore
	authority *internalremoteworker.CertificateAuthority
}

func NewRemoteWorkerEnrollmentHTTPServer(verifier AccessTokenVerifier, store remoteWorkerEnrollmentStore, authority *internalremoteworker.CertificateAuthority) (*RemoteWorkerEnrollmentHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("remote worker enrollment HTTP server configuration is invalid")
	}
	return &RemoteWorkerEnrollmentHTTPServer{verifier: verifier, store: store, authority: authority}, nil
}

func (server *RemoteWorkerEnrollmentHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	tenantID, projectID, enrollmentID, action, ok := remoteWorkerEnrollmentPath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, 404, "route_not_found")
		return
	}
	if action == "issue-certificate" || action == "rotate-certificate" || action == "heartbeat" {
		if request.Method != http.MethodPost {
			writePublicProblem(writer, 405, "method_not_allowed")
			return
		}
		requestID, requestOK := exactSingleHeader(request.Header, "X-Request-ID")
		if !requestOK {
			writePublicProblem(writer, 400, "invalid_request")
			return
		}
		if action == "issue-certificate" {
			server.issueCertificate(writer, request, tenantID, projectID, enrollmentID, requestID)
		} else if action == "rotate-certificate" {
			server.rotateCertificate(writer, request, tenantID, projectID, enrollmentID, requestID)
		} else {
			server.heartbeat(writer, request, tenantID, projectID, enrollmentID, requestID)
		}
		return
	}
	projectPermission, enrollmentPermission, allowed := remoteWorkerEnrollmentPermission(action, request.Method)
	if !allowed {
		writePublicProblem(writer, 405, "method_not_allowed")
		return
	}
	requestID, requestOK := exactSingleHeader(request.Header, "X-Request-ID")
	authorization, authorizationOK := exactSingleHeader(request.Header, "Authorization")
	bearer, bearerOK := bearerToken(authorization)
	if !requestOK {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	if !authorizationOK || !bearerOK {
		writePublicProblem(writer, 401, "authentication_failed")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, projectPermission)
	if err != nil {
		writePublicProblem(writer, 401, "authentication_failed")
		return
	}
	if _, err := server.verify(bearer, tenantID, projectID, enrollmentPermission); err != nil {
		writePublicProblem(writer, 403, "authorization_denied")
		return
	}
	switch action {
	case "collection":
		if request.Method == http.MethodPost {
			server.create(writer, request, tenantID, projectID, requestID, principal)
		} else {
			server.list(writer, request, tenantID, projectID, requestID, principal)
		}
	case "get":
		server.get(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
	case "revoke":
		server.revoke(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
	case "scheduling-preview":
		server.schedulingPreview(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
	case "scheduling":
		server.transitionScheduling(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
	case "claim-secret":
		server.claimSecret(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
	case "audit-events":
		server.listAuditEvents(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
	case "operations":
		server.listOperations(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
	}
}

func (server *RemoteWorkerEnrollmentHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	validated, err := openapiv1alpha1.ValidateListAdminRemoteWorkerEnrollmentsServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if !ok || err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	after := ""
	if validated.PageToken != "" {
		after, ok = decodeProjectResourcePageToken("remote-worker-enrollment/v1", tenantID, projectID, validated.PageToken)
		if !ok {
			writePublicProblem(writer, 400, "invalid_request")
			return
		}
	}
	page, err := server.store.ListRemoteWorkerEnrollments(request.Context(), tenantID, principal, projectID, after, validated.PageSize)
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	values := make([]platformv1alpha1.RemoteWorkerEnrollment, 0, len(page.Enrollments))
	for _, value := range page.Enrollments {
		values = append(values, remoteWorkerEnrollmentResource(value))
	}
	next := ""
	if page.NextEnrollmentID != "" {
		next, ok = encodeProjectResourcePageToken("remote-worker-enrollment/v1", tenantID, projectID, page.NextEnrollmentID)
		if !ok {
			writePublicProblem(writer, 500, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeRemoteWorkerEnrollmentPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RemoteWorkerEnrollmentPage]{Value: platformv1alpha1.RemoteWorkerEnrollmentPage{APIVersion: platformv1alpha1.APIVersion, Kind: "RemoteWorkerEnrollmentPage", RemoteWorkerEnrollments: values, NextPageToken: next}})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writeJSONResponse(writer, 200, requestID, body)
}

func (server *RemoteWorkerEnrollmentHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := remoteWorkerEnrollmentMutationRequest(writer, request)
	if !ok {
		return
	}
	validated, err := openapiv1alpha1.ValidateCreateAdminRemoteWorkerEnrollmentServerRequest(tenantID, projectID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	value, err := server.store.CreateRemoteWorkerEnrollment(request.Context(), tenantID, principal, internalremoteworker.CreateInput{
		Scope: internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}, EnrollmentID: validated.Body.EnrollmentID,
		WorkerID: validated.Body.WorkerID, WorkerName: validated.Body.WorkerName, TTLSeconds: validated.Body.TTLSeconds,
		Mutation: internalremoteworker.Mutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	writeRemoteWorkerEnrollment(writer, 201, requestID, value)
}

func (server *RemoteWorkerEnrollmentHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string, principal *authn.VerifiedPrincipal) {
	if _, err := openapiv1alpha1.ValidateGetAdminRemoteWorkerEnrollmentServerRequest(tenantID, projectID, enrollmentID, requestID); err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	value, err := server.store.GetRemoteWorkerEnrollment(request.Context(), tenantID, principal, projectID, enrollmentID)
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	writeRemoteWorkerEnrollment(writer, 200, requestID, value)
}

func (server *RemoteWorkerEnrollmentHTTPServer) revoke(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := remoteWorkerEnrollmentMutationRequest(writer, request)
	if !ok {
		return
	}
	validated, err := openapiv1alpha1.ValidateRevokeAdminRemoteWorkerEnrollmentServerRequest(tenantID, projectID, enrollmentID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	version, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	value, err := server.store.RevokeRemoteWorkerEnrollment(request.Context(), tenantID, principal, internalremoteworker.TransitionInput{
		Scope: internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}, EnrollmentID: enrollmentID,
		ExpectedResourceVersion: version, ConfirmedEnrollmentID: validated.Body.ConfirmedEnrollmentID,
		Mutation: internalremoteworker.Mutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	writeRemoteWorkerEnrollment(writer, 200, requestID, value)
}

func (server *RemoteWorkerEnrollmentHTTPServer) schedulingPreview(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string, principal *authn.VerifiedPrincipal) {
	if _, err := openapiv1alpha1.ValidatePreviewAdminRemoteWorkerSchedulingServerRequest(tenantID, projectID, enrollmentID, requestID); err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	preview, err := server.store.PreviewRemoteWorkerScheduling(request.Context(), tenantID, principal, projectID, enrollmentID)
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	node := preview.Enrollment.Node
	if node == nil {
		writePublicProblem(writer, 409, "remote_worker_node_unavailable")
		return
	}
	value := platformv1alpha1.RemoteWorkerNodeSchedulingPreview{ResourceBase: platformv1alpha1.ResourceBase{
		APIVersion: platformv1alpha1.APIVersion, Kind: "RemoteWorkerNodeSchedulingPreview",
		Metadata: commonv1alpha1.ResourceMetadata{UID: preview.Enrollment.EnrollmentID, Name: preview.Enrollment.WorkerName,
			TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: tenantID},
			ResourceVersion: strconv.FormatInt(node.ResourceVersion, 10),
			CreatedAt:       preview.Enrollment.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: preview.Enrollment.UpdatedAt.UTC().Format(time.RFC3339Nano)},
	}, Spec: platformv1alpha1.RemoteWorkerNodeSchedulingPreviewSpec{
		ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID},
		WorkerID:   preview.Enrollment.WorkerID, HealthState: node.HealthState,
		CurrentDesiredState: node.DesiredState, CurrentObservedState: node.ObservedState,
		DesiredState: preview.DesiredState, ExpectedGeneration: node.Generation,
		ExpectedResourceVersion: strconv.FormatInt(node.ResourceVersion, 10), ImpactDigest: preview.ImpactDigest,
		ImpactSummary: preview.ImpactSummary, CommandDeadlineSeconds: int64(internalremoteworker.CommandTTL / time.Second),
	}}
	body, err := platformv1alpha1.EncodeRemoteWorkerNodeSchedulingPreviewResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RemoteWorkerNodeSchedulingPreview]{Value: value})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", value.Metadata.ResourceVersion)
	writeJSONResponse(writer, 200, requestID, body)
}

func (server *RemoteWorkerEnrollmentHTTPServer) transitionScheduling(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := remoteWorkerEnrollmentMutationRequest(writer, request)
	if !ok {
		return
	}
	validated, err := openapiv1alpha1.ValidateTransitionAdminRemoteWorkerSchedulingServerRequest(tenantID, projectID, enrollmentID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	version, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	operation, err := server.store.TransitionRemoteWorkerScheduling(request.Context(), tenantID, principal, internalremoteworker.SchedulingInput{
		Scope: internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}, EnrollmentID: enrollmentID,
		ExpectedGeneration: validated.Body.ExpectedGeneration, ExpectedResourceVersion: version,
		ConfirmedEnrollmentID: validated.Body.ConfirmedEnrollmentID, DesiredState: validated.Body.DesiredState,
		ImpactDigest: validated.Body.ImpactDigest, Mutation: internalremoteworker.Mutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	writeRemoteWorkerOperation(writer, 200, requestID, operation)
}

func (server *RemoteWorkerEnrollmentHTTPServer) claimSecret(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string, principal *authn.VerifiedPrincipal) {
	key, body, ok := remoteWorkerEnrollmentMutationRequest(writer, request)
	if !ok {
		return
	}
	validated, err := openapiv1alpha1.ValidateClaimRemoteWorkerEnrollmentSecretServerRequest(tenantID, projectID, enrollmentID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	version, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	secret, err := internalremoteworker.NewEnrollmentSecret()
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	secretDigest, err := internalremoteworker.SecretDigest(secret)
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	value, err := server.store.ClaimRemoteWorkerEnrollmentSecret(request.Context(), tenantID, principal, internalremoteworker.TransitionInput{
		Scope: internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}, EnrollmentID: enrollmentID,
		ExpectedResourceVersion: version, ConfirmedEnrollmentID: validated.Body.ConfirmedEnrollmentID,
		Mutation: internalremoteworker.Mutation{RequestID: requestID, IdempotencyKey: key},
	}, secretDigest)
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	body, err = platformv1alpha1.EncodeRemoteWorkerEnrollmentSecretResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RemoteWorkerEnrollmentSecret]{Value: platformv1alpha1.RemoteWorkerEnrollmentSecret{
		APIVersion: platformv1alpha1.APIVersion, Kind: "RemoteWorkerEnrollmentSecret",
		ProjectRef:   commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID},
		EnrollmentID: enrollmentID, EnrollmentSecret: secret, ExpiresAt: value.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writeJSONResponse(writer, 200, requestID, body)
}

func (server *RemoteWorkerEnrollmentHTTPServer) issueCertificate(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string) {
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	secret, ok := remoteWorkerEnrollmentAuthorization(authorization)
	if !ok {
		writePublicProblem(writer, 401, "authentication_failed")
		return
	}
	key, body, ok := remoteWorkerEnrollmentMutationRequest(writer, request)
	if !ok {
		return
	}
	validated, err := openapiv1alpha1.ValidateIssueRemoteWorkerCertificateServerRequest(tenantID, projectID, enrollmentID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	version, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	secretDigest, err := internalremoteworker.SecretDigest(secret)
	if err != nil {
		writePublicProblem(writer, 401, "authentication_failed")
		return
	}
	if err := server.store.AuthenticateRemoteWorkerCertificate(request.Context(), tenantID, projectID, enrollmentID, secretDigest, version); err != nil {
		if errors.Is(err, postgres.ErrRemoteWorkerEnrollmentAuthentication) {
			writePublicProblem(writer, 401, "authentication_failed")
		} else {
			writePublicProblem(writer, 500, "internal_error")
		}
		return
	}
	if server.authority == nil {
		writePublicProblem(writer, 503, "remote_worker_certificate_authority_unavailable")
		return
	}
	certificate, err := server.authority.Issue(internalremoteworker.CertificateInput{Scope: internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}, EnrollmentID: enrollmentID, IncarnationID: validated.Body.IncarnationID, CSRPEM: validated.Body.CertificateSigningRequestPEM})
	if err != nil {
		writeRemoteWorkerCertificateAuthorityError(writer, err)
		return
	}
	actorDigest, err := internalremoteworker.BootstrapActorDigest(secretDigest)
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	value, err := server.store.IssueRemoteWorkerCertificate(request.Context(), tenantID, internalremoteworker.CertificatePersistenceInput{
		Scope: internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}, EnrollmentID: enrollmentID,
		ExpectedResourceVersion: version, ConfirmedEnrollmentID: validated.Body.ConfirmedEnrollmentID,
		SecretDigest: secretDigest, ActorDigest: actorDigest, Certificate: certificate,
		Mutation: internalremoteworker.Mutation{RequestID: requestID, IdempotencyKey: key},
	})
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	writeRemoteWorkerCertificate(writer, requestID, projectID, enrollmentID, value, value.EnrolledAt)
}

func (server *RemoteWorkerEnrollmentHTTPServer) rotateCertificate(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string) {
	if server.authority == nil {
		writePublicProblem(writer, 503, "remote_worker_certificate_authority_unavailable")
		return
	}
	peer, err := server.authority.PeerIdentity(request.TLS)
	if err != nil || peer.Scope.TenantID != tenantID || peer.Scope.ProjectID != projectID || peer.EnrollmentID != enrollmentID {
		writePublicProblem(writer, 401, "authentication_failed")
		return
	}
	key, body, ok := remoteWorkerEnrollmentMutationRequest(writer, request)
	if !ok {
		return
	}
	validated, err := openapiv1alpha1.ValidateRotateRemoteWorkerCertificateServerRequest(tenantID, projectID, enrollmentID, requestID, key, body)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	version, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	rotation := internalremoteworker.CertificateRotationRequest{
		Scope: internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}, EnrollmentID: enrollmentID,
		ExpectedResourceVersion: version, ConfirmedEnrollmentID: validated.Body.ConfirmedEnrollmentID,
		PeerCertificateSHA256: peer.CertificateSHA256, IncarnationID: validated.Body.IncarnationID,
		CSRPEM:   validated.Body.CertificateSigningRequestPEM,
		Mutation: internalremoteworker.Mutation{RequestID: requestID, IdempotencyKey: key},
	}
	authorized, err := server.store.AuthorizeRemoteWorkerCertificateRotation(request.Context(), tenantID, rotation)
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	if !authorized.NeedsSigning {
		writeRemoteWorkerCertificate(writer, requestID, projectID, enrollmentID, authorized.Enrollment, authorized.Enrollment.CertificateNotBefore)
		return
	}
	certificate, err := server.authority.Issue(internalremoteworker.CertificateInput{Scope: rotation.Scope, EnrollmentID: enrollmentID, IncarnationID: rotation.IncarnationID, CSRPEM: rotation.CSRPEM})
	if err != nil {
		writeRemoteWorkerCertificateAuthorityError(writer, err)
		return
	}
	value, err := server.store.RotateRemoteWorkerCertificate(request.Context(), tenantID, internalremoteworker.CertificateRotationPersistenceInput{Request: rotation, Certificate: certificate})
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	writeRemoteWorkerCertificate(writer, requestID, projectID, enrollmentID, value, value.CertificateNotBefore)
}

func (server *RemoteWorkerEnrollmentHTTPServer) heartbeat(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string) {
	if server.authority == nil {
		writePublicProblem(writer, 503, "remote_worker_certificate_authority_unavailable")
		return
	}
	peer, err := server.authority.PeerIdentity(request.TLS)
	if err != nil || peer.Scope.TenantID != tenantID || peer.Scope.ProjectID != projectID || peer.EnrollmentID != enrollmentID {
		writePublicProblem(writer, 401, "authentication_failed")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 8<<20))
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateHeartbeatRemoteWorkerServerRequest(tenantID, projectID, enrollmentID, requestID, body)
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	if validated.Body.IncarnationID != peer.IncarnationID {
		writePublicProblem(writer, 401, "authentication_failed")
		return
	}
	result, err := server.store.HeartbeatRemoteWorker(request.Context(), tenantID, internalremoteworker.HeartbeatInput{
		Scope: peer.Scope, EnrollmentID: enrollmentID, PeerCertificateSHA256: peer.CertificateSHA256,
		IncarnationID: validated.Body.IncarnationID, ObservedGeneration: validated.Body.ObservedGeneration,
		ObservedState: validated.Body.ObservedState,
		WorkerVersion: validated.Body.WorkerVersion, OS: validated.Body.OS, Architecture: validated.Body.Architecture,
		KernelVersion: validated.Body.KernelVersion, Capabilities: validated.Body.Capabilities,
		Capacity:                  internalremoteworker.Capacity{CPUMillis: validated.Body.Capacity.CPUMillis, MemoryBytes: validated.Body.Capacity.MemoryBytes, DiskBytes: validated.Body.Capacity.DiskBytes},
		CommandReceipt:            remoteWorkerCommandReceipt(validated.Body.CommandReceipt),
		SandboxCommandReceipt:     remoteWorkerSandboxCommandReceipt(validated.Body.SandboxCommandReceipt),
		SandboxExecCommandReceipt: remoteWorkerSandboxExecCommandReceipt(validated.Body.SandboxExecCommandReceipt),
		SandboxFileCommandReceipt: validated.Body.SandboxFileCommandReceipt,
		SandboxPTYCommandReceipt:  validated.Body.SandboxPTYCommandReceipt,
	})
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	node := result.Node
	responseBody, err := platformv1alpha1.EncodeRemoteWorkerHeartbeatResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RemoteWorkerHeartbeat]{Value: platformv1alpha1.RemoteWorkerHeartbeat{
		APIVersion: platformv1alpha1.APIVersion, Kind: "RemoteWorkerHeartbeat",
		ProjectRef:   commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID},
		EnrollmentID: enrollmentID, WorkerID: node.WorkerID, IncarnationID: node.IncarnationID,
		Generation: node.Generation, ObservedGeneration: node.ObservedGeneration,
		DesiredState: node.DesiredState, ObservedState: node.ObservedState, HealthState: node.HealthState,
		AcceptedAt: node.LastHeartbeatAt.UTC().Format(time.RFC3339Nano), ExpiresAt: node.HeartbeatExpiresAt.UTC().Format(time.RFC3339Nano),
		NextHeartbeatAfterSeconds: int64(internalremoteworker.HeartbeatInterval / time.Second), ReconcileRequired: result.ReconcileRequired,
		Command:            remoteWorkerCommandResource(result.Command),
		SandboxCommand:     remoteWorkerSandboxCommandResource(result.SandboxCommand),
		SandboxExecCommand: remoteWorkerSandboxExecCommandResource(result.SandboxExecCommand),
		SandboxFileCommand: result.SandboxFileCommand,
		SandboxPTYCommand:  result.SandboxPTYCommand,
	}})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writeJSONResponse(writer, 200, requestID, responseBody)
}

func remoteWorkerSandboxExecCommandReceipt(value *platformv1alpha1.RemoteWorkerSandboxExecCommandReceipt) *internalremoteworker.SandboxExecCommandReceipt {
	if value == nil {
		return nil
	}
	return &internalremoteworker.SandboxExecCommandReceipt{CommandID: value.CommandID, SandboxID: value.SandboxID,
		SandboxGeneration: value.SandboxGeneration, Result: value.Result, ExitCode: value.ExitCode,
		Stdout: value.Stdout, Stderr: value.Stderr, ExecutionTimeMillis: value.ExecutionTimeMillis,
		StableErrorCode: value.StableErrorCode}
}

func remoteWorkerSandboxCommandReceipt(value *platformv1alpha1.RemoteWorkerSandboxCommandReceipt) *internalremoteworker.SandboxCommandReceipt {
	if value == nil {
		return nil
	}
	return &internalremoteworker.SandboxCommandReceipt{CommandID: value.CommandID, Attempt: value.Attempt, Action: value.Action,
		OperationID: value.OperationID, SandboxID: value.SandboxID, SandboxGeneration: value.SandboxGeneration,
		Result: value.Result, RuntimeID: value.RuntimeID, RuntimeState: value.RuntimeState,
		VolumeName: value.VolumeName, StableErrorCode: value.StableErrorCode, CleanupComplete: value.CleanupComplete}
}

func remoteWorkerCommandReceipt(value *platformv1alpha1.RemoteWorkerCommandReceipt) *internalremoteworker.CommandReceipt {
	if value == nil {
		return nil
	}
	return &internalremoteworker.CommandReceipt{CommandID: value.CommandID, Generation: value.Generation, Result: value.Result, StableErrorCode: value.StableErrorCode}
}

func remoteWorkerCommandResource(value *internalremoteworker.Command) *platformv1alpha1.RemoteWorkerCommand {
	if value == nil {
		return nil
	}
	return &platformv1alpha1.RemoteWorkerCommand{CommandID: value.CommandID, Generation: value.Generation, DesiredState: value.DesiredState, Deadline: value.Deadline.UTC().Format(time.RFC3339Nano)}
}

func remoteWorkerSandboxCommandResource(value *internalremoteworker.SandboxCommand) *platformv1alpha1.RemoteWorkerSandboxCommand {
	if value == nil {
		return nil
	}
	return &platformv1alpha1.RemoteWorkerSandboxCommand{CommandID: value.CommandID, Attempt: value.Attempt,
		Action: value.Action, OperationID: value.OperationID, WorkspaceID: value.WorkspaceID,
		WorkspaceName: value.WorkspaceName, TargetID: value.TargetID, SandboxID: value.SandboxID,
		SandboxGeneration: value.SandboxGeneration, ImageURI: value.ImageURI, CPUMillis: value.CPUMillis,
		MemoryBytes: value.MemoryBytes, SpecDigest: value.SpecDigest, NetworkPolicyID: value.NetworkPolicyID,
		NetworkAllowedEgress: value.NetworkAllowedEgress, PhysicalVolumeName: value.PhysicalVolumeName,
		RuntimeID: value.RuntimeID, RuntimeState: value.RuntimeState, RuntimeOperationID: value.RuntimeOperationID,
		RuntimeGeneration: value.RuntimeGeneration, RuntimeSpecDigest: value.RuntimeSpecDigest,
		Deadline: value.Deadline.UTC().Format(time.RFC3339Nano)}
}

func remoteWorkerSandboxExecCommandResource(value *internalremoteworker.SandboxExecCommand) *platformv1alpha1.RemoteWorkerSandboxExecCommand {
	if value == nil {
		return nil
	}
	return &platformv1alpha1.RemoteWorkerSandboxExecCommand{CommandID: value.CommandID, WorkspaceID: value.WorkspaceID,
		TargetID: value.TargetID, SandboxID: value.SandboxID, SandboxGeneration: value.SandboxGeneration,
		RuntimeID: value.RuntimeID, RuntimeOperationID: value.RuntimeOperationID,
		RuntimeSpecDigest: value.RuntimeSpecDigest, Command: value.Command, TimeoutSeconds: value.TimeoutSeconds,
		Deadline: value.Deadline.UTC().Format(time.RFC3339Nano)}
}

func writeRemoteWorkerCertificate(writer http.ResponseWriter, requestID, projectID, enrollmentID string, value internalremoteworker.Snapshot, issuedAt *time.Time) {
	if issuedAt == nil || value.CertificateNotAfter == nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	responseBody, err := platformv1alpha1.EncodeRemoteWorkerCertificateResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RemoteWorkerCertificate]{Value: platformv1alpha1.RemoteWorkerCertificate{
		APIVersion: platformv1alpha1.APIVersion, Kind: "RemoteWorkerCertificate",
		ProjectRef:   commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID},
		EnrollmentID: enrollmentID, WorkerID: value.WorkerID, IncarnationID: value.IncarnationID,
		SPIFFEID: value.SPIFFEID, CertificateChainPEM: value.CertificateChainPEM, CertificateSHA256: value.CertificateSHA256,
		IssuedAt: issuedAt.UTC().Format(time.RFC3339Nano), ExpiresAt: value.CertificateNotAfter.UTC().Format(time.RFC3339Nano),
	}})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writeJSONResponse(writer, 200, requestID, responseBody)
}

func writeRemoteWorkerCertificateAuthorityError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, internalremoteworker.ErrInvalidCertificateRequest):
		writePublicProblem(writer, 400, "invalid_remote_worker_certificate_request")
	case errors.Is(err, internalremoteworker.ErrCertificateAuthorityUnavailable):
		writePublicProblem(writer, 503, "remote_worker_certificate_authority_unavailable")
	default:
		writePublicProblem(writer, 500, "internal_error")
	}
}

func (server *RemoteWorkerEnrollmentHTTPServer) listAuditEvents(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	validated, err := openapiv1alpha1.ValidateListAdminRemoteWorkerEnrollmentAuditEventsServerRequest(tenantID, projectID, enrollmentID, requestID, pageSize, pageToken)
	if !ok || err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	var after *time.Time
	afterID := ""
	if validated.PageToken != "" {
		after, afterID, ok = decodeRemoteWorkerEnrollmentAuditPageToken(tenantID, projectID, enrollmentID, validated.PageToken)
		if !ok {
			writePublicProblem(writer, 400, "invalid_request")
			return
		}
	}
	page, err := server.store.ListRemoteWorkerEnrollmentAuditEvents(request.Context(), tenantID, principal, projectID, enrollmentID, after, afterID, validated.PageSize)
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	events := make([]platformv1alpha1.AdminAuditEvent, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, platformv1alpha1.AdminAuditEvent{APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEvent", EventID: event.EventID, Actor: event.Actor, Action: event.Action, ResourceKind: "RemoteWorkerEnrollment", ResourceID: event.EnrollmentID, ResourceGeneration: event.ResourceGeneration, Result: event.Result, OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano), RequestID: event.RequestID, OperationID: event.OperationID, StableErrorCode: event.StableErrorCode})
	}
	next := ""
	if page.NextOccurredAt != nil {
		next, ok = encodeRemoteWorkerEnrollmentAuditPageToken(tenantID, projectID, enrollmentID, *page.NextOccurredAt, page.NextEventID)
		if !ok {
			writePublicProblem(writer, 500, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeAdminAuditEventPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.AdminAuditEventPage]{Value: platformv1alpha1.AdminAuditEventPage{APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEventPage", Events: events, NextPageToken: next}})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writeJSONResponse(writer, 200, requestID, body)
}

func (server *RemoteWorkerEnrollmentHTTPServer) listOperations(writer http.ResponseWriter, request *http.Request, tenantID, projectID, enrollmentID, requestID string, principal *authn.VerifiedPrincipal) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	validated, err := openapiv1alpha1.ValidateListAdminRemoteWorkerOperationsServerRequest(tenantID, projectID, enrollmentID, requestID, pageSize, pageToken)
	if !ok || err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return
	}
	var after *time.Time
	afterID := ""
	if validated.PageToken != "" {
		after, afterID, ok = decodeRemoteWorkerOperationPageToken(tenantID, projectID, enrollmentID, validated.PageToken)
		if !ok {
			writePublicProblem(writer, 400, "invalid_request")
			return
		}
	}
	page, err := server.store.ListRemoteWorkerOperations(request.Context(), tenantID, principal, projectID, enrollmentID, after, afterID, validated.PageSize)
	if err != nil {
		writeRemoteWorkerEnrollmentError(writer, err)
		return
	}
	values := make([]platformv1alpha1.MaintenanceOperation, 0, len(page.Operations))
	for _, operation := range page.Operations {
		values = append(values, remoteWorkerOperationResource(operation))
	}
	next := ""
	if page.NextRequestedAt != nil {
		next, ok = encodeRemoteWorkerOperationPageToken(tenantID, projectID, enrollmentID, *page.NextRequestedAt, page.NextOperationID)
		if !ok {
			writePublicProblem(writer, 500, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeMaintenanceOperationPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.MaintenanceOperationPage]{Value: platformv1alpha1.MaintenanceOperationPage{APIVersion: platformv1alpha1.APIVersion, Kind: "MaintenanceOperationPage", Operations: values, NextPageToken: next}})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writeJSONResponse(writer, 200, requestID, body)
}

func remoteWorkerOperationResource(operation internalremoteworker.Operation) platformv1alpha1.MaintenanceOperation {
	return platformv1alpha1.MaintenanceOperation{APIVersion: platformv1alpha1.APIVersion, Kind: "MaintenanceOperation",
		OperationID: operation.OperationID, IdempotencyKey: operation.IdempotencyKey, Action: operation.Action,
		ResourceKind: "RemoteWorkerEnrollment", ResourceID: operation.EnrollmentID, ResourceGeneration: operation.Generation,
		RequestedBy: operation.RequestedBy, RequestID: operation.RequestID,
		RequestedAt: operation.RequestedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: operation.UpdatedAt.UTC().Format(time.RFC3339Nano),
		State: operation.State, CurrentStep: operation.CurrentStep, StableErrorCode: operation.StableErrorCode,
		ImpactSummary: operation.ImpactSummary, Retryable: operation.Retryable}
}

func writeRemoteWorkerOperation(writer http.ResponseWriter, status int, requestID string, operation internalremoteworker.Operation) {
	body, err := platformv1alpha1.EncodeMaintenanceOperationResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.MaintenanceOperation]{Value: remoteWorkerOperationResource(operation)})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writeJSONResponse(writer, status, requestID, body)
}

func remoteWorkerEnrollmentMutationRequest(writer http.ResponseWriter, request *http.Request) (string, []byte, bool) {
	key, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writePublicProblem(writer, 400, "invalid_request")
		return "", nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, 400, "invalid_request")
		return "", nil, false
	}
	return key, body, true
}

func (server *RemoteWorkerEnrollmentHTTPServer) verify(bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func remoteWorkerEnrollmentResource(value internalremoteworker.Snapshot) platformv1alpha1.RemoteWorkerEnrollment {
	format := func(value *time.Time) string {
		if value == nil {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	return platformv1alpha1.RemoteWorkerEnrollment{ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "RemoteWorkerEnrollment", Metadata: commonv1alpha1.ResourceMetadata{
		UID: value.EnrollmentID, Name: value.WorkerName, TenantRef: commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: value.Scope.TenantID},
		ResourceVersion: strconv.FormatInt(value.ResourceVersion, 10), CreatedAt: value.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: value.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platformv1alpha1.RemoteWorkerEnrollmentSpec{ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: value.Scope.ProjectID}, TargetID: value.TargetID, WorkerID: value.WorkerID, State: value.State, ExpiresAt: value.ExpiresAt.UTC().Format(time.RFC3339Nano), SecretClaimedAt: format(value.SecretClaimedAt), EnrolledAt: format(value.EnrolledAt), RevokedAt: format(value.RevokedAt), IncarnationID: value.IncarnationID, SPIFFEID: value.SPIFFEID, CertificateSHA256: value.CertificateSHA256, CertificateExpiresAt: format(value.CertificateNotAfter), CertificateState: value.CertificateState, CertificateRevokedAt: format(value.CertificateRevokedAt), Node: remoteWorkerNodeStatusResource(value.Node)}}
}

func remoteWorkerNodeStatusResource(value *internalremoteworker.NodeStatus) *platformv1alpha1.RemoteWorkerNodeStatus {
	if value == nil {
		return nil
	}
	return &platformv1alpha1.RemoteWorkerNodeStatus{
		ResourceVersion: strconv.FormatInt(value.ResourceVersion, 10), Generation: value.Generation,
		ObservedGeneration: value.ObservedGeneration, DesiredState: value.DesiredState,
		ObservedState: value.ObservedState, HealthState: value.HealthState, WorkerVersion: value.WorkerVersion,
		OS: value.OS, Architecture: value.Architecture, KernelVersion: value.KernelVersion,
		Capabilities:       value.Capabilities,
		Capacity:           platformv1alpha1.RemoteWorkerCapacity{CPUMillis: value.Capacity.CPUMillis, MemoryBytes: value.Capacity.MemoryBytes, DiskBytes: value.Capacity.DiskBytes},
		FirstConnectedAt:   value.FirstConnectedAt.UTC().Format(time.RFC3339Nano),
		LastHeartbeatAt:    value.LastHeartbeatAt.UTC().Format(time.RFC3339Nano),
		HeartbeatExpiresAt: value.HeartbeatExpiresAt.UTC().Format(time.RFC3339Nano),
	}
}

func writeRemoteWorkerEnrollment(writer http.ResponseWriter, status int, requestID string, value internalremoteworker.Snapshot) {
	body, err := platformv1alpha1.EncodeRemoteWorkerEnrollmentResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RemoteWorkerEnrollment]{Value: remoteWorkerEnrollmentResource(value)})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(value.ResourceVersion, 10))
	writeJSONResponse(writer, status, requestID, body)
}

func remoteWorkerEnrollmentPath(path string) (tenantID, projectID, enrollmentID, action string, ok bool) {
	prefix := adminEnvironmentProfileRoutePrefix
	isBootstrap := false
	isRemoteWorker := false
	if strings.HasPrefix(path, "/v1/remote-worker-bootstrap/tenants/") {
		prefix = "/v1/remote-worker-bootstrap/tenants/"
		isBootstrap = true
	} else if strings.HasPrefix(path, "/v1/remote-workers/tenants/") {
		prefix = "/v1/remote-workers/tenants/"
		isRemoteWorker = true
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "remote-worker-enrollments" && parts[0] != "" && parts[2] != "" {
		if isBootstrap || isRemoteWorker {
			return "", "", "", "", false
		}
		return parts[0], parts[2], "", "collection", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "remote-worker-enrollments" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		if strings.HasSuffix(parts[4], ":scheduling-preview") && !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":scheduling-preview"), "scheduling-preview", true
		}
		if strings.HasSuffix(parts[4], ":scheduling") && !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":scheduling"), "scheduling", true
		}
		if strings.HasSuffix(parts[4], ":revoke") && !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":revoke"), "revoke", true
		}
		if strings.HasSuffix(parts[4], ":claimSecret") && isBootstrap {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":claimSecret"), "claim-secret", true
		}
		if strings.HasSuffix(parts[4], ":issueCertificate") && isBootstrap {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":issueCertificate"), "issue-certificate", true
		}
		if strings.HasSuffix(parts[4], ":rotateCertificate") && isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":rotateCertificate"), "rotate-certificate", true
		}
		if strings.HasSuffix(parts[4], ":heartbeat") && isRemoteWorker {
			return parts[0], parts[2], strings.TrimSuffix(parts[4], ":heartbeat"), "heartbeat", true
		}
		if !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], parts[4], "get", true
		}
	}
	if len(parts) == 6 && !isBootstrap && !isRemoteWorker && parts[1] == "projects" && parts[3] == "remote-worker-enrollments" && parts[4] != "" {
		if parts[5] == "audit-events" || parts[5] == "operations" {
			return parts[0], parts[2], parts[4], parts[5], true
		}
	}
	return "", "", "", "", false
}

func remoteWorkerEnrollmentAuthorization(value string) (string, bool) {
	const prefix = "RemoteWorkerEnrollment "
	if !strings.HasPrefix(value, prefix) || strings.ContainsAny(value, "\r\n") {
		return "", false
	}
	secret := strings.TrimPrefix(value, prefix)
	_, err := internalremoteworker.SecretDigest(secret)
	return secret, err == nil
}

func remoteWorkerEnrollmentPermission(action, method string) (string, string, bool) {
	switch {
	case action == "collection" && method == http.MethodGet:
		return "projects.get", "remote-worker-enrollments.list", true
	case action == "collection" && method == http.MethodPost:
		return "projects.act", "remote-worker-enrollments.create", true
	case action == "get" && method == http.MethodGet:
		return "projects.get", "remote-worker-enrollments.get", true
	case action == "revoke" && method == http.MethodPost:
		return "projects.act", "remote-worker-enrollments.act", true
	case action == "scheduling-preview" && method == http.MethodGet:
		return "projects.get", "remote-worker-enrollments.get", true
	case action == "scheduling" && method == http.MethodPost:
		return "projects.act", "remote-worker-enrollments.act", true
	case action == "claim-secret" && method == http.MethodPost:
		return "projects.act", "remote-worker-bootstrap.act", true
	case action == "audit-events" && method == http.MethodGet:
		return "projects.get", "audit.list", true
	case action == "operations" && method == http.MethodGet:
		return "projects.get", "operations.list", true
	default:
		return "", "", false
	}
}

func HandlesRemoteWorkerEnrollmentPath(path string) bool {
	_, _, _, _, ok := remoteWorkerEnrollmentPath(path)
	return ok
}

func encodeRemoteWorkerOperationPageToken(tenantID, projectID, enrollmentID string, requestedAt time.Time, operationID string) (string, bool) {
	if requestedAt.IsZero() || commonv1alpha1.ValidateIdentifier(operationID, "/operationId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("remote-worker-operation/v1\x00" + tenantID + "\x00" + projectID + "\x00" + enrollmentID + "\x00" + requestedAt.UTC().Format(time.RFC3339Nano) + "\x00" + operationID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeRemoteWorkerOperationPageToken(tenantID, projectID, enrollmentID, token string) (*time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 || parts[0] != "remote-worker-operation/v1" || parts[1] != tenantID || parts[2] != projectID || parts[3] != enrollmentID || commonv1alpha1.ValidateIdentifier(parts[5], "/operationId") != nil {
		return nil, "", false
	}
	requestedAt, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil {
		return nil, "", false
	}
	return &requestedAt, parts[5], true
}

func encodeRemoteWorkerEnrollmentAuditPageToken(tenantID, projectID, enrollmentID string, occurredAt time.Time, eventID string) (string, bool) {
	if occurredAt.IsZero() || commonv1alpha1.ValidateIdentifier(eventID, "/eventId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("remote-worker-enrollment-audit/v1\x00" + tenantID + "\x00" + projectID + "\x00" + enrollmentID + "\x00" + occurredAt.UTC().Format(time.RFC3339Nano) + "\x00" + eventID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeRemoteWorkerEnrollmentAuditPageToken(tenantID, projectID, enrollmentID, token string) (*time.Time, string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 || parts[0] != "remote-worker-enrollment-audit/v1" || parts[1] != tenantID || parts[2] != projectID || parts[3] != enrollmentID || commonv1alpha1.ValidateIdentifier(parts[5], "/eventId") != nil {
		return nil, "", false
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil {
		return nil, "", false
	}
	return &occurredAt, parts[5], true
}

func writeRemoteWorkerEnrollmentError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrRemoteWorkerEnrollmentNotFound):
		writePublicProblem(writer, 404, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, 403, "authorization_denied")
	case errors.Is(err, postgres.ErrRemoteWorkerEnrollmentIdempotencyConflict):
		writePublicProblem(writer, 409, "idempotency_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerEnrollmentResourceVersionConflict):
		writePublicProblem(writer, 409, "remote_worker_enrollment_resource_version_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerGenerationConflict):
		writePublicProblem(writer, 409, "remote_worker_generation_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerCommandReceiptConflict):
		writePublicProblem(writer, 409, "remote_worker_command_receipt_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerSandboxReceiptConflict):
		writePublicProblem(writer, 409, "remote_worker_sandbox_receipt_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerSandboxExecReceiptConflict):
		writePublicProblem(writer, 409, "remote_worker_sandbox_exec_receipt_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerSandboxFileReceiptConflict):
		writePublicProblem(writer, 409, "remote_worker_sandbox_file_receipt_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerSandboxPTYReceiptConflict):
		writePublicProblem(writer, 409, "remote_worker_sandbox_pty_receipt_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerSchedulingIdempotencyConflict):
		writePublicProblem(writer, 409, "idempotency_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerOperationInProgress):
		writePublicProblem(writer, 409, "operation_in_progress")
	case errors.Is(err, postgres.ErrRemoteWorkerSchedulingResourceVersionConflict):
		writePublicProblem(writer, 409, "remote_worker_resource_version_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerSchedulingStateConflict):
		writePublicProblem(writer, 409, "remote_worker_scheduling_state_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerSchedulingImpactConflict):
		writePublicProblem(writer, 409, "scheduling_impact_conflict")
	case errors.Is(err, postgres.ErrRemoteWorkerNodeUnavailable):
		writePublicProblem(writer, 409, "remote_worker_node_unavailable")
	case errors.Is(err, postgres.ErrRemoteWorkerEnrollmentSecretUnavailable):
		writePublicProblem(writer, 409, "remote_worker_enrollment_secret_unavailable")
	case errors.Is(err, postgres.ErrRemoteWorkerEnrollmentAuthentication):
		writePublicProblem(writer, 401, "authentication_failed")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, 400, "invalid_request")
	default:
		writePublicProblem(writer, 500, "internal_error")
	}
}
