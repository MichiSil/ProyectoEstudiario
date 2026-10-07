# Estudiario

Planificador de estudio para estudiantes universitarios, construido como sistema basado en microservicios para el Práctico Integrador de **Arquitectura de Software 2026** (Facultad de Ingeniería – UCC).

## Integrantes

| Legajo | Integrante |
|---|---|
| 2405404 | Acuña, Isabela |
| 2418092 | Chamaza, Florencia |
| 2411741 | Silvestrini, Mia |

## Dominio

Un estudiante cursa materias, cada una con exámenes (parciales, finales, recuperatorios, entregas) y una dificultad percibida. Además declara en qué franjas horarias de la semana puede estudiar y cuántos minutos por día como máximo. Estudiario toma esa información y **reserva sesiones de estudio en la agenda del estudiante** para llegar preparado a cada examen.

| Dominio | Entidad principal | Acción principal | Característica relevante |
|---|---|---|---|
| Planificación de estudio universitario | Examen / sesión de estudio | Confirmar el plan de estudio de un examen (reservar sus sesiones) | Las sesiones no pueden superponerse ni superar la capacidad diaria del estudiante; una misma confirmación no puede duplicarse; si el examen cambia de fecha, el plan queda desactualizado y debe replanificarse |

## Objetivo

Que el estudiante no tenga que decidir a mano cuándo estudiar cada materia: el sistema propone un plan a partir de la fecha del examen, la dificultad de la materia, los temas a estudiar y la disponibilidad horaria, y al confirmarlo garantiza que la agenda resultante sea consistente.

## Alcance preliminar

- Registro e inicio de sesión de estudiantes.
- Gestión de cuatrimestres, materias, exámenes y notas.
- Disponibilidad semanal (franjas horarias y máximo de minutos por día).
- Generación y confirmación de planes de estudio por examen.
- Apuntes por materia, con búsqueda.
- Una capacidad publicada para que otro grupo pueda generar planes de estudio desde su propio sistema.
