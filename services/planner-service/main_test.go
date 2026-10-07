package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthRespondeOK(t *testing.T) {
	rec := httptest.NewRecorder()
	newRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, se esperaba application/json", ct)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("el cuerpo no es JSON válido: %v", err)
	}
	if body.Status != "ok" || body.Service != serviceName || body.ContractVersion != contractVersion {
		t.Errorf("cuerpo = %+v, se esperaba status ok, service %s y contractVersion %s", body, serviceName, contractVersion)
	}
}

func TestHealthRechazaOtrosMetodos(t *testing.T) {
	rec := httptest.NewRecorder()
	newRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/health", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
