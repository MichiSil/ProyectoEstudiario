// Package mcpserver expone planner-service como servidor MCP (Model Context
// Protocol) para que un agente de IA opere Estudiario (SPEC 10.1).
//
// Las herramientas llaman a los mismos casos de uso que la API REST, así que
// valen las mismas reglas de negocio. El agente siempre actúa en nombre del
// estudiante autenticado (RN-36): el servidor se arma por pedido con el
// X-User-Id que puso el api-gateway después de validar el JWT. No hay
// herramienta para confirmar: el agente propone, el estudiante confirma (RN-37).
package mcpserver

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// Version es la versión del servidor MCP que ve el agente.
const Version = "1.0.0"

type materia struct {
	ID         string   `json:"id"`
	Nombre     string   `json:"nombre"`
	Profesores []string `json:"profesores"`
	// Dificultad de 1 (fácil) a 4 (muy difícil); define con cuántos días de anticipación se empieza a estudiar (RN-06).
	Dificultad int `json:"dificultad"`
}

type examen struct {
	ID        string   `json:"id"`
	MateriaID string   `json:"materiaId"`
	Materia   string   `json:"materia"`
	Tipo      string   `json:"tipo"`
	Fecha     string   `json:"fecha"` // RFC 3339, con la zona del estudiante
	Temas     []string `json:"temas"`
	// Modalidad: "teorico", "practico", "mixto" o vacía si no se indicó.
	Modalidad string `json:"modalidad,omitempty"`
	// EstadoPlan: "confirmado", "desactualizado" o vacío si todavía no tiene plan.
	EstadoPlan string `json:"estadoPlan,omitempty"`
}

type sesionPropuesta struct {
	Inicio       string `json:"inicio"`
	Fin          string `json:"fin"`
	Minutos      int    `json:"minutos"`
	Tema         string `json:"tema"`
	Objetivo     string `json:"objetivo"`
	Razonamiento string `json:"razonamiento,omitempty"`
}

type sinArgumentos struct{}

type listaMaterias struct {
	Materias []materia `json:"materias"`
}

type listaExamenes struct {
	Examenes []examen `json:"examenes"`
}

type proponerPlanArgs struct {
	ExamenID string `json:"examenId" jsonschema:"el id del examen, tal como lo devuelve listar_examenes"`
}

type propuesta struct {
	PlanID     string            `json:"planId"`
	Estado     string            `json:"estado"`
	ArmadaPor  string            `json:"armadaPor"` // "ia" o "reglas"
	Avisos     []string          `json:"avisos"`
	Sesiones   []sesionPropuesta `json:"sesiones"`
	Importante string            `json:"importante"`
}

// New arma el servidor MCP de un estudiante.
func New(svc *app.Service, studentID string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "estudiario-planner", Version: Version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "listar_materias",
		Description: "Devuelve las materias del estudiante, con sus profesores y su dificultad (1 fácil a 4 muy difícil).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ sinArgumentos) (*mcp.CallToolResult, listaMaterias, error) {
		subjects, err := svc.ListSubjects(ctx, studentID)
		if err != nil {
			return nil, listaMaterias{}, err
		}
		out := listaMaterias{Materias: make([]materia, 0, len(subjects))}
		for _, sub := range subjects {
			out.Materias = append(out.Materias, materia{ID: sub.ID, Nombre: sub.Name, Profesores: sub.Professors, Dificultad: int(sub.Difficulty)})
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "listar_examenes",
		Description: "Devuelve los próximos exámenes programados del estudiante (no los cancelados ni los que ya pasaron), con su fecha, sus temas y si ya tienen plan.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ sinArgumentos) (*mcp.CallToolResult, listaExamenes, error) {
		exams, err := svc.ListExams(ctx, studentID)
		if err != nil {
			return nil, listaExamenes{}, err
		}
		subjects, err := svc.ListSubjects(ctx, studentID)
		if err != nil {
			return nil, listaExamenes{}, err
		}
		names := make(map[string]string, len(subjects))
		for _, sub := range subjects {
			names[sub.ID] = sub.Name
		}
		now := time.Now()
		out := listaExamenes{Examenes: []examen{}}
		for _, e := range exams {
			if e.Status != domain.ExamScheduled || e.StartsAt.Before(now) {
				continue
			}
			topics := e.Topics
			if topics == nil {
				topics = []string{}
			}
			out.Examenes = append(out.Examenes, examen{
				ID: e.ID, MateriaID: e.SubjectID, Materia: names[e.SubjectID], Tipo: string(e.Type),
				Fecha: e.StartsAt.Format(time.RFC3339), Temas: topics, Modalidad: string(e.Modality), EstadoPlan: string(e.PlanStatus),
			})
		}
		sort.Slice(out.Examenes, func(i, j int) bool { return out.Examenes[i].Fecha < out.Examenes[j].Fecha })
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "proponer_plan",
		Description: "Pide una propuesta de plan de estudio para un examen (RF-08). La propuesta NO reserva nada: " +
			"para que las sesiones queden en el calendario, el estudiante tiene que confirmarla él mismo en Estudiario.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in proponerPlanArgs) (*mcp.CallToolResult, propuesta, error) {
		plan, err := svc.ProposePlan(ctx, studentID, in.ExamenID)
		if err != nil {
			return nil, propuesta{}, err
		}
		out := propuesta{
			PlanID: plan.ID, Estado: string(plan.Status), ArmadaPor: string(plan.GeneratedBy), Avisos: plan.Warnings,
			Sesiones:   make([]sesionPropuesta, 0, len(plan.Drafts)),
			Importante: "Es sólo una propuesta: el estudiante la revisa y la confirma desde la pantalla Plan de Estudiario.",
		}
		if out.Avisos == nil {
			out.Avisos = []string{}
		}
		for _, d := range plan.Drafts {
			out.Sesiones = append(out.Sesiones, sesionPropuesta{
				Inicio: d.Start.Format(time.RFC3339), Fin: d.End().Format(time.RFC3339), Minutos: d.DurationMinutes,
				Tema: d.Topic, Objetivo: d.Objective, Razonamiento: d.Reasoning,
			})
		}
		return nil, out, nil
	})

	return s
}

// Handler atiende el MCP por HTTP (transporte "streamable HTTP") para un
// estudiante. Es sin estado: cada pedido arma su servidor con la identidad que
// validó el gateway, así nunca se mezclan estudiantes entre pedidos.
func Handler(svc *app.Service, studentID string) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return New(svc, studentID)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
