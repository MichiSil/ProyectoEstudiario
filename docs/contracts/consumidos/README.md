# Contratos que consume Estudiario

Copias de los contratos de otros grupos que Estudiario **implementa como consumidor**. Son la referencia del test de contrato: si el proveedor cambia su contrato de forma incompatible, el test lo detecta.

| Archivo | Proveedor | Versión | Copiado de | Fecha |
|---|---|---|---|---|
| [`club-beneficios-v2.yaml`](club-beneficios-v2.yaml) | Club de Beneficios de Metalfitness (Grupo 1) | 2.0.0 | [`docs/contracts/benefits-api.v2.yaml`](https://github.com/martiipastore/Proyecto-ARQUI-SW---GIMNASIO/blob/main/docs/contracts/benefits-api.v2.yaml), commit `94887b0` | 2026-11-09 |

- **Quién lo consume:** users-service ([`internal/club`](../../../services/users-service/internal/club)). La decisión está en el [ADR-015](../../adr/ADR-015-club-de-beneficios.md).
- **Guía del proveedor:** [README del Club](https://github.com/martiipastore/Proyecto-ARQUI-SW---GIMNASIO/blob/main/docs/contracts/README.md).
- **Mock:** `docker compose up club-mock` levanta Prism sobre esta copia en `http://localhost:4011` (dentro de Docker, `http://club-mock:4010`).

## Test de contrato

Contra la copia:

```bash
cd services/users-service
go test -run Contrato ./internal/club/
```

Contra la copia y el contrato publicado (necesita internet):

```bash
cd services/users-service
CLUB_CONTRACT_LIVE=1 go test -run Contrato -v ./internal/club/
```

## Cuando el proveedor publica una versión nueva

1. Correr el test con `CLUB_CONTRACT_LIVE=1`. Si pasa, el cambio es compatible con lo que usamos.
2. Si falla, avisarle al Grupo 1 qué se rompió y seguir usando la versión que implementamos mientras la mantengan.
3. Actualizar el adaptador y reemplazar la copia, con la versión, el commit y la fecha en esta tabla.
