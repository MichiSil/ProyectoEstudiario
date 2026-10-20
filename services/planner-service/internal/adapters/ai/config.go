package ai

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Valores por defecto: la API de Groq compatible con OpenAI y el presupuesto
// de 8 s del ADR-005.
const (
	DefaultBaseURL = "https://api.groq.com/openai/v1"
	DefaultModel   = "openai/gpt-oss-120b"
	DefaultTimeout = 8 * time.Second
)

// ErrMissingAPIKey indica que no se configuró AI_API_KEY. Sin clave, el
// servicio funciona sólo con el motor de reglas.
var ErrMissingAPIKey = errors.New("falta la variable de entorno AI_API_KEY")

// Config es la configuración del cliente del proveedor de IA.
type Config struct {
	APIKey  string
	Model   string
	BaseURL string
	// Timeout es el presupuesto total de la llamada, reintento incluido.
	Timeout time.Duration
}

// ConfigFromEnv lee la configuración de AI_API_KEY, AI_MODEL, AI_BASE_URL y
// AI_TIMEOUT (por ejemplo "8s"). Sólo la clave es obligatoria.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		APIKey:  strings.TrimSpace(os.Getenv("AI_API_KEY")),
		Model:   strings.TrimSpace(os.Getenv("AI_MODEL")),
		BaseURL: strings.TrimSpace(os.Getenv("AI_BASE_URL")),
		Timeout: DefaultTimeout,
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if raw := strings.TrimSpace(os.Getenv("AI_TIMEOUT")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("AI_TIMEOUT inválido %q: tiene que ser una duración positiva, por ejemplo 8s", raw)
		}
		cfg.Timeout = d
	}
	if cfg.APIKey == "" {
		return cfg, ErrMissingAPIKey
	}
	return cfg, nil
}
