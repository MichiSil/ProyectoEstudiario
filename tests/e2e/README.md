# Tests end-to-end

Recorren el flujo principal del plan de estudio de punta a punta (SPEC 10.2, CA-30) contra el sistema levantado con `docker compose`, siempre a través del api-gateway y con el JWT de users-service, como el frontend. No simulan ningún servicio interno.

| Test | Qué verifica |
|---|---|
| `TestFlujoPrincipal` | 1) registro e inicio de sesión, 2) materia, examen y disponibilidad, 3) propuesta sin reservar nada (RN-08), 4) confirmación con las sesiones en el calendario (CA-06) y 5) reintento con la misma `Idempotency-Key` sin duplicar (CA-10) |
| `TestPropuestaConIANoDisponible` | 6) con la IA caída, la propuesta llega armada por reglas con el aviso `AI_FALLBACK` (CA-05) |
| `TestAislamientoEntreEstudiantes` | Un estudiante no ve ni planifica los exámenes de otro (RN-03) |
| `TestSinTokenRechaza` | Sin JWT el gateway responde 401, aunque el cliente mande `X-User-Id` |

## Cómo correrlos

Desde la raíz del repo, con el `.env` copiado de `.env.example`:

```bash
# 1. Levantar el sistema desde cero, con la IA apuntada a una dirección que no responde (paso 6)
docker compose -f docker-compose.yml -f tests/e2e/docker-compose.e2e.yml up -d --build

# 2. Correr los tests contra el api-gateway
cd tests/e2e
E2E_BASE_URL=http://localhost:8080 E2E_AI_DOWN=1 go test -count=1 -v ./...
```

Sin `E2E_AI_DOWN=1` (por ejemplo, con el sistema levantado normal y la IA de Groq andando), el paso 6 se omite y el resto se corre igual. Sin `E2E_BASE_URL`, todos se omiten: así `go test ./...` no falla en una compu sin el sistema levantado.

Cada test crea su propio estudiante con un mail único, así que se pueden correr varias veces sobre la misma base.
