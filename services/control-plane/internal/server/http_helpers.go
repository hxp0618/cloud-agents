package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

const publicFallbackRequestID = "request-unknown"

func ConcurrentRequestLimitHandler(limit int, next http.Handler) http.Handler {
	slots := make(chan struct{}, limit)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" || request.URL.Path == "/readyz" {
			next.ServeHTTP(writer, request)
			return
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
