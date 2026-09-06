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
	AuthenticateRemoteWorkerCertificate(context.Context, string, string, string, string, int64) error
	AuthorizeRemoteWorkerCertificateRotation(context.Context, string, internalremoteworker.CertificateRotationRequest) (postgres.RemoteWorkerCertificateRotationAuthorization, error)
	IssueRemoteWorkerCertificate(context.Context, string, internalremoteworker.CertificatePersistenceInput) (internalremoteworker.Snapshot, error)
	RotateRemoteWorkerCertificate(context.Context, string, internalremoteworker.CertificateRotationPersistenceInput) (internalremoteworker.Snapshot, error)
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
	if action == "issue-certificate" || action == "rotate-certificate" {
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
		} else {
			server.rotateCertificate(writer, request, tenantID, projectID, enrollmentID, requestID)
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
	case "claim-secret":
		server.claimSecret(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
	case "audit-events":
		server.listAuditEvents(writer, request, tenantID, projectID, enrollmentID, requestID, principal)
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
		events = append(events, platformv1alpha1.AdminAuditEvent{APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEvent", EventID: event.EventID, Actor: event.Actor, Action: event.Action, ResourceKind: "RemoteWorkerEnrollment", ResourceID: event.EnrollmentID, ResourceGeneration: event.EnrollmentResourceVersion, Result: event.Result, OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano), RequestID: event.RequestID, OperationID: event.OperationID})
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
	}}, Spec: platformv1alpha1.RemoteWorkerEnrollmentSpec{ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: value.Scope.ProjectID}, WorkerID: value.WorkerID, State: value.State, ExpiresAt: value.ExpiresAt.UTC().Format(time.RFC3339Nano), SecretClaimedAt: format(value.SecretClaimedAt), EnrolledAt: format(value.EnrolledAt), RevokedAt: format(value.RevokedAt), IncarnationID: value.IncarnationID, SPIFFEID: value.SPIFFEID, CertificateSHA256: value.CertificateSHA256, CertificateExpiresAt: format(value.CertificateNotAfter), CertificateState: value.CertificateState, CertificateRevokedAt: format(value.CertificateRevokedAt)}}
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
		if !isBootstrap && !isRemoteWorker {
			return parts[0], parts[2], parts[4], "get", true
		}
	}
	if len(parts) == 6 && !isBootstrap && !isRemoteWorker && parts[1] == "projects" && parts[3] == "remote-worker-enrollments" && parts[4] != "" && parts[5] == "audit-events" {
		return parts[0], parts[2], parts[4], "audit-events", true
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
	case action == "claim-secret" && method == http.MethodPost:
		return "projects.act", "remote-worker-bootstrap.act", true
	case action == "audit-events" && method == http.MethodGet:
		return "projects.get", "audit.list", true
	default:
		return "", "", false
	}
}

func HandlesRemoteWorkerEnrollmentPath(path string) bool {
	_, _, _, _, ok := remoteWorkerEnrollmentPath(path)
	return ok
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
