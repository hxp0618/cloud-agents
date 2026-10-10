package server

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"strings"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
)

const publicFallbackRequestID = "request-unknown"

type contextAccessTokenVerifier interface {
	VerifyContext(context.Context, string, authn.VerificationRequest) (*authn.VerifiedPrincipal, error)
}

func verifyHTTPRequestAccessToken(ctx context.Context, verifier AccessTokenVerifier, token string, request authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	if contextual, ok := verifier.(contextAccessTokenVerifier); ok {
		return contextual.VerifyContext(ctx, token, request)
	}
	return verifier.Verify(token, request)
}

func JSONContentTypeHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && request.ContentLength != 0 {
			values := request.Header.Values("Content-Type")
			mediaType, _, err := mime.ParseMediaType(strings.Join(values, ","))
			if len(values) != 1 || err != nil || mediaType != "application/json" {
				preparePublicRequestID(writer, request)
				writePublicProblem(writer, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
				return
			}
		}
		next.ServeHTTP(writer, request)
	})
}

func ConcurrentRequestLimitHandler(limit int, next http.Handler) http.Handler {
	ordinarySlots := make(chan struct{}, limit)
	executionSlots := make(chan struct{}, limit)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" || request.URL.Path == "/readyz" {
			next.ServeHTTP(writer, request)
			return
		}
		slots := ordinarySlots
		if request.Method == http.MethodPost {
			_, _, _, _, _, action, ok := managedAgentExecutionPath(request.URL.Path)
			if ok && action == "execute" {
				slots = executionSlots
			}
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(writer, request)
		default:
			preparePublicRequestID(writer, request)
			writer.Header().Set("Retry-After", "1")
			writePublicProblem(writer, http.StatusTooManyRequests, "REQUEST_CAPACITY_EXHAUSTED")
		}
	})
}

type publicProblem struct {
	Type      string            `json:"type"`
	Title     string            `json:"title"`
	Status    int               `json:"status"`
	Error     publicStableError `json:"error"`
	RequestID string            `json:"requestId"`
}

type publicStableError struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func preparePublicRequestID(writer http.ResponseWriter, request *http.Request) {
	if writer == nil {
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	requestID := publicFallbackRequestID
	if request != nil {
		if value, ok := exactSingleHeader(request.Header, "X-Request-ID"); ok {
			requestID = value
		}
	}
	writer.Header().Set("X-Request-ID", requestID)
}

func writePublicProblem(writer http.ResponseWriter, status int, code string) {
	if writer == nil {
		return
	}
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusInternalServerError
	}
	stableCode := stableProblemCode(code)
	requestID := writer.Header().Get("X-Request-ID")
	if requestID == "" {
		requestID = publicFallbackRequestID
		writer.Header().Set("X-Request-ID", requestID)
	}
	problem := publicProblem{
		Type:      "https://problems.cloud-agents.dev/" + strings.ToLower(strings.ReplaceAll(stableCode, "_", "-")),
		Title:     publicProblemTitle(stableCode),
		Status:    status,
		Error:     publicStableError{Code: stableCode, Retryable: status == http.StatusTooManyRequests || status >= http.StatusInternalServerError},
		RequestID: requestID,
	}
	if status == http.StatusUnauthorized {
		writer.Header().Set("WWW-Authenticate", "Bearer")
	}
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(problem)
}

func stableProblemCode(code string) string {
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", "_"))
	if code == "" {
		return "INTERNAL_ERROR"
	}
	return code
}

func publicProblemTitle(code string) string {
	switch code {
	case "INVALID_REQUEST":
		return "Invalid request"
	case "AUTHENTICATION_FAILED":
		return "Authentication failed"
	case "AUTHORIZATION_DENIED":
		return "Authorization denied"
	case "NOT_FOUND", "RESOURCE_NOT_FOUND":
		return "Resource not found"
	case "ROUTE_NOT_FOUND":
		return "Route not found"
	case "METHOD_NOT_ALLOWED":
		return "Method not allowed"
	case "REQUEST_CAPACITY_EXHAUSTED":
		return "Request capacity exhausted"
	case "UNSUPPORTED_MEDIA_TYPE":
		return "Unsupported media type"
	default:
		return "Cloud Agents request failed"
	}
}

func exactSingleHeader(header http.Header, name string) (string, bool) {
	values := header.Values(name)
	return firstExactValue(values)
}

func firstExactValue(values []string) (string, bool) {
	if len(values) != 1 || values[0] == "" || strings.TrimSpace(values[0]) != values[0] {
		return "", false
	}
	return values[0], true
}
