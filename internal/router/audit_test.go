package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditLog_CapturesStatus(t *testing.T) {
	handler := AuditLog(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
}

func TestAuditLog_DefaultsTo200(t *testing.T) {
	handler := AuditLog(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestStatusWriter_Unwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: 200}

	inner := sw.Unwrap()
	if inner != rec {
		t.Fatal("Unwrap should return the underlying ResponseWriter")
	}
}
