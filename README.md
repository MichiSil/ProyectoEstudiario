# Estudiario

Planificador de estudio para estudiantes universitarios, construido como un sistema basado en microservicios para el Práctico Integrador de **Arquitectura de Software 2026** (Facultad de Ingeniería – UCC).

## Integrantes

| Legajo | Integrante |
|---|---|
| 2405404 | Acuña, Isabela |
| 2418092 | Chamaza, Florencia |
| 2411741 | Silvestrini, Mia |

## Dominio

Un estudiante cursa materias en un cuatrimestre. Cada materia tiene una **dificultad** (fácil, media, difícil o muy difícil) y una serie de **exámenes**: parciales, finales, recuperatorios o entregas, cada uno con su fecha y los temas que entran. El estudiante también declara su **disponibilidad**: en qué franjas de la semana puede estudiar y cuántos minutos por día como máximo.

Con esa información, Estudiario **reserva sesiones de estudio en la agenda del estudiante** para que llegue preparado a cada examen.

| Dominio | Entidad principal | Acción principal | Característica relevante |
|---|---|---|---|
| Planificación de estudio universitario | Examen / sesión de estudio | Confirmar el plan de estudio de un examen (reservar sus sesiones) | Las sesiones no pueden superponerse ni superar la capacidad diaria del estudiante; una misma confirmación no puede duplicarse; si el examen cambia de fecha, el plan queda desactualizado y hay que replanificarlo |

La acción principal no es una simple alta de registros. Para confirmar un plan, el sistema tiene que validar varias restricciones al mismo tiempo, y esa operación puede llegar en paralelo desde dos dispositivos del mismo estudiante o repetirse por un reintento de red.

## Objetivo

Que el estudiante no tenga que decidir a mano cuándo estudiar cada materia. El sistema propone un plan a partir de:

- la fecha del examen y los temas que incluye,
- la dificultad de la materia (las más difíciles empiezan a estudiarse con más anticipación),
- el rendimiento previo del estudiante en esa materia,
- la disponibilidad horaria declarada.

Al confirmarlo, el sistema garantiza que la agenda resultante sea consistente: sin sesiones superpuestas, sin días sobrecargados y sin sesiones duplicadas.

## Flujo principal

1. **Ingreso.** El estudiante se registra o inicia sesión.
2. **Materias.** Da de alta las materias del cuatrimestre con su dificultad.
3. **Exámenes.** Carga un examen: materia, tipo, fecha y temas.
4. **Disponibilidad.** Indica sus franjas horarias semanales y el máximo de minutos de estudio por día.
5. **Propuesta.** Pide un plan para ese examen y el sistema le propone sesiones con fecha, horario, duración, tema y objetivo. Por ejemplo: "Leer teoría de Integrales", "Resolver ejercicios", "Simulacro de parcial".
6. **Confirmación (acción principal).** El estudiante confirma el plan. Las sesiones quedan reservadas sólo si:
   - todas caen dentro de una franja disponible y antes del examen;
   - ninguna se superpone con otra sesión ya reservada;
   - ningún día supera el máximo de minutos declarado;
   - la confirmación no fue procesada antes (si se repite, se devuelve el mismo resultado en lugar de duplicar sesiones).
7. **Cambios.** Si el examen cambia de fecha o se cancela, el plan pasa a *desactualizado* y las sesiones pendientes se replanifican o se liberan.
8. **Seguimiento.** A medida que estudia, el estudiante marca cada sesión como completada o pospuesta.

## Cómo levantar el sistema

Todo el sistema (servicios, bases de datos y mensajería) se levanta con **un único comando** usando Docker, sin configuraciones manuales.

### Requisitos

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (incluye Docker Compose v2).
- Git.

### Pasos

```bash
git clone https://github.com/MichiSil/ProyectoEstudiario.git
cd ProyectoEstudiario
cp .env.example .env        # variables locales; los secretos reales nunca se suben al repo
docker compose up --build
```

Para detenerlo:

```bash
docker compose down         # agregar -v para borrar también los datos locales
```

> Los servicios se van incorporando entrega a entrega. Esta sección se actualiza junto con el `docker-compose.yml` cada vez que se suma un componente nuevo, e indicará los puertos y URLs de acceso.

### Entorno desplegado

La capacidad que publicamos para otros grupos va a estar desplegada en la nube con una URL pública. La vamos a publicar acá cuando esté disponible.

## Documentación

| Documento | Contenido |
|---|---|
| [`SPEC.md`](SPEC.md) | Alcance, funcionalidades, reglas de negocio y criterios de aceptación |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Arquitectura general, servicios, datos, comunicaciones y diagramas |
| [`docs/adr/`](docs/adr/) | Registro de decisiones arquitectónicas (ADR) |
| [`docs/contracts/`](docs/contracts/) | Contrato versionado de la capacidad publicada para otros grupos |
| [`docs/POSTMORTEM.md`](docs/POSTMORTEM.md) | Informe de la caída controlada |

## Forma de trabajo

- `main` contiene siempre la última versión estable y evaluable.
- Cada tarea se desarrolla en su propia rama (`feature/...`, `docs/...`, `fix/...`) y se integra a `main` con un *pull request* revisado por otra integrante.
