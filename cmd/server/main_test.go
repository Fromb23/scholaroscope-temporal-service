package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithCORSCredentialedAllowedOrigin(t *testing.T) {
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []string{"https://scholaroscope.com"})

	req := httptest.NewRequest(http.MethodOptions, "/portal/launch/exchange", nil)
	req.Header.Set("Origin", "https://scholaroscope.com")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Scholaroscope-Timestamp, X-Scholaroscope-Signature")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://scholaroscope.com" {
		t.Fatalf("unexpected allow origin %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected credentialed CORS, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "*" || got == "" {
		t.Fatalf("expected explicit CORS headers, got %q", got)
	}
}

func TestWithCORSRejectsWildcardCredentialPattern(t *testing.T) {
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []string{"https://scholaroscope.com"})

	req := httptest.NewRequest(http.MethodOptions, "/portal/launch/exchange", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected CORS allow origin for disallowed origin")
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("unexpected credentialed CORS for disallowed origin")
	}
}
