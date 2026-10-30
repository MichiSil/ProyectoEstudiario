package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// baseURL es el api-gateway del sistema levantado con docker compose. Los tests
// hablan sólo con él, como el frontend: no se simula ningún servicio interno.
func baseURL(t *testing.T) string {
	t.Helper()
	url := strings.TrimRight(os.Getenv("E2E_BASE_URL"), "/")
	if url == "" {
		t.Skip("E2E_BASE_URL no definida: los tests end-to-end necesitan el sistema levantado (ver tests/e2e/README.md)")
	}
	return url
}

// client es un estudiante: guarda su token y manda cada pedido por el gateway.
type client struct {
	t     *testing.T
	base  string
	token string
	http  *http.Client
}

func newClient(t *testing.T) *client {
	// 15 s: por encima del timeout de 12 s del gateway para la propuesta.
	return &client{t: t, base: baseURL(t), http: &http.Client{Timeout: 15 * time.Second}}
}

type response struct {
	Status int
	Body   []byte
	Header http.Header
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("respuesta no es JSON válido (%v): %s", err, r.Body)
	}
}

func (c *client) do(method, path string, body any, headers map[string]string) response {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Correlation-Id", fmt.Sprintf("e2e-%s-%d", c.t.Name(), time.Now().UnixNano()))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return response{Status: res.StatusCode, Body: raw, Header: res.Header}
}

// expect hace el pedido y falla el test si el status no es el esperado.
func (c *client) expect(status int, method, path string, body any, headers map[string]string) response {
	c.t.Helper()
	res := c.do(method, path, body, headers)
	if res.Status != status {
		c.t.Fatalf("%s %s: se esperaba %d y llegó %d: %s", method, path, status, res.Status, res.Body)
	}
	return res
}
