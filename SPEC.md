# Especificación de Estudiario

Este documento define **qué** tiene que hacer el sistema: su alcance, las funcionalidades, las reglas de negocio y los criterios de aceptación. **Cómo** se implementa está en [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) y en los [ADR](docs/adr/).

Es un documento vivo: se actualiza en cada entrega a medida que el alcance se precisa.

## 1. Alcance

### 1.1 Incluido

| Área | Qué abarca |
|---|---|
| **Cuentas** | Registro, inicio de sesión y roles (estudiante y administrador). |
| **Organización del estudio** | Materias propias con dificultad, calendario de exámenes y entregas, disponibilidad horaria semanal. |
| **Plan de estudio** | Propuesta de sesiones generada por un agente de IA, confirmación del plan (acción principal), replanificación ante cambios y seguimiento de sesiones. |
| **Foro de resúmenes** | Publicación de resúmenes, moderación por el administrador, búsqueda de resúmenes aprobados y calificación. |
| **Notificaciones** | Mail al autor cuando su resumen es aprobado o rechazado. |
| **Capacidad para otro grupo** | Generar un plan de estudio a partir de un examen y una disponibilidad, a través de un contrato publicado en [`docs/contracts/`](docs/contracts/). |

### 1.2 Fuera de alcance

- Pagos, suscripciones o cualquier tipo de cobro.
- Aplicaciones móviles nativas (el acceso es por la interfaz web).
- Mensajería entre estudiantes o comentarios en los resúmenes.
- Recuperación de contraseña por mail e inicio de sesión con proveedores externos (Google, etc.).
- Edición de un resumen ya moderado: para corregirlo, el autor sube una nueva publicación.
- Integración con calendarios externos (Google Calendar, Outlook).

## 2. Actores

| Actor | Descripción |
|---|---|
| **Estudiante** | Usuario registrado. Gestiona su organización de estudio, pide y confirma planes, sube resúmenes y califica los de otros. |
| **Administrador** | Usuario con rol de moderación. Aprueba, rechaza o elimina publicaciones del foro. |
| **Agente de IA** | Componente del sistema que propone las sesiones de un plan de estudio. No toma decisiones finales: el estudiante siempre confirma. |
| **Sistema externo consumidor** | Sistema de otro grupo que usa la capacidad publicada de generación de planes. |

## 3. Glosario

| Término | Definición |
|---|---|
| **Materia** | Asignatura que cursa un estudiante. Pertenece a un único estudiante y tiene una dificultad. |
| **Dificultad** | Valoración de la materia hecha por el estudiante: 1 fácil, 2 media, 3 difícil, 4 muy difícil. Influye en cuánto antes empieza a estudiarse. |
| **Examen** | Fecha importante de una materia: parcial, final, recuperatorio o entrega. Tiene fecha, hora opcional y temas. |
| **Franja disponible** | Intervalo semanal en el que el estudiante puede estudiar (por ejemplo, lunes de 18:00 a 21:00). |
| **Capacidad diaria** | Máximo de minutos de estudio por día que declara el estudiante. |
| **Sesión de estudio** | Bloque de estudio con fecha, horario, duración, tema y objetivo, asociado a un examen. |
| **Propuesta de plan** | Conjunto de sesiones sugeridas para un examen. Todavía no ocupa lugar en el calendario. |
| **Plan confirmado** | Propuesta aceptada por el estudiante: sus sesiones quedan reservadas en el calendario. |
| **Plan desactualizado** | Plan confirmado cuyo examen cambió de fecha o fue cancelado. |
| **Resumen / publicación** | Material de estudio que un estudiante sube al foro, asociado a una materia. |
| **Moderación** | Decisión del administrador sobre una publicación: aprobarla, rechazarla o eliminarla. |
| **Calificación** | Puntaje de 1 a 5 que un estudiante le da a un resumen aprobado. |

## 4. Convenciones

- **RF-XX**: requisito funcional.
- **RN-XX**: regla de negocio. Es una restricción que el sistema tiene que hacer cumplir siempre, venga de donde venga la operación.
- **CA-XX**: criterio de aceptación, escrito como *Dado / Cuando / Entonces*.
