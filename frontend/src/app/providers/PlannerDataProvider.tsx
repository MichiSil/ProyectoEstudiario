import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { plannerApi } from '@/api/planner'
import { ApiError, errorMessage } from '@/api/client'
import type { Availability, Exam, Session, Subject } from '@/api/types'
import { readJson, writeJson } from '@/lib/storage'
import { useSession } from './SessionProvider'

/**
 * Datos de planificación del estudiante.
 *
 * - Materias, exámenes y sesiones se leen siempre de planner-service
 *   (GET /subjects, GET /exams y GET /sessions): el backend es la única
 *   fuente de verdad.
 * - La disponibilidad también se lee del servidor (GET /availability), así se
 *   ve la misma en todos los dispositivos. La copia en el navegador sólo sirve
 *   para mostrarla al instante mientras llega la del servidor.
 */

interface PlannerData {
  subjects: Subject[]
  sessions: Session[]
  exams: Exam[]
  availability: Availability | null
  loading: boolean
  error: string | null
  subjectsById: Map<string, Subject>
  examsById: Map<string, Exam>
  refresh: () => Promise<void>
  refreshSessions: () => Promise<void>
  addSubject: (subject: Subject) => void
  /** Reemplaza una materia con lo que devolvió el servidor al editarla. */
  rememberSubject: (subject: Subject) => void
  /** Reemplaza una sesión con lo que devolvió el servidor al marcarla. */
  rememberSession: (session: Session) => void
  /** Agrega o reemplaza un examen con lo que devolvió el servidor. */
  rememberExam: (exam: Exam) => void
  rememberAvailability: (availability: Availability) => void
}

const PlannerDataContext = createContext<PlannerData | null>(null)

export function PlannerDataProvider({ children }: { children: ReactNode }) {
  const { user } = useSession()
  const availabilityKey = `estudiario-availability:${user?.id ?? ''}`

  const [subjects, setSubjects] = useState<Subject[]>([])
  const [sessions, setSessions] = useState<Session[]>([])
  const [exams, setExams] = useState<Exam[]>([])
  const [availability, setAvailability] = useState<Availability | null>(() => readJson(availabilityKey, null))
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setAvailability(readJson(availabilityKey, null))
  }, [availabilityKey])

  const refresh = useCallback(async () => {
    if (!user) return
    setError(null)
    try {
      const [subjectList, examList, sessionList, savedAvailability] = await Promise.all([
        plannerApi.listSubjects(),
        plannerApi.listExams(),
        plannerApi.listSessions(),
        // Sin disponibilidad cargada, planner-service responde 404: no es un error de la pantalla.
        plannerApi.getAvailability().catch((err: unknown) => {
          if (err instanceof ApiError && err.status === 404) return null
          return undefined // otro error: se deja la copia local
        }),
      ])
      setSubjects(subjectList ?? [])
      setExams(examList ?? [])
      setSessions(sessionList ?? [])
      if (savedAvailability !== undefined) {
        if (savedAvailability) writeJson(availabilityKey, savedAvailability)
        setAvailability(savedAvailability)
      }
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setLoading(false)
    }
  }, [user, availabilityKey])

  const refreshSessions = useCallback(async () => {
    try {
      setSessions((await plannerApi.listSessions()) ?? [])
    } catch (err) {
      setError(errorMessage(err))
    }
  }, [])

  useEffect(() => {
    setLoading(true)
    void refresh()
  }, [refresh])

  const addSubject = useCallback((subject: Subject) => {
    setSubjects((prev) => [...prev, subject])
  }, [])

  const rememberSubject = useCallback((subject: Subject) => {
    setSubjects((prev) => prev.map((s) => (s.id === subject.id ? subject : s)))
  }, [])

  const rememberSession = useCallback((session: Session) => {
    setSessions((prev) => prev.map((s) => (s.id === session.id ? session : s)))
  }, [])

  const rememberExam = useCallback((exam: Exam) => {
    setExams((prev) => [...prev.filter((e) => e.id !== exam.id), exam])
  }, [])

  const rememberAvailability = useCallback(
    (next: Availability) => {
      writeJson(availabilityKey, next)
      setAvailability(next)
    },
    [availabilityKey],
  )

  const value = useMemo<PlannerData>(
    () => ({
      subjects,
      sessions,
      exams,
      availability,
      loading,
      error,
      subjectsById: new Map(subjects.map((s) => [s.id, s])),
      examsById: new Map(exams.map((e) => [e.id, e])),
      refresh,
      refreshSessions,
      addSubject,
      rememberSubject,
      rememberSession,
      rememberExam,
      rememberAvailability,
    }),
    [subjects, sessions, exams, availability, loading, error, refresh, refreshSessions, addSubject, rememberSubject, rememberSession, rememberExam, rememberAvailability],
  )

  return <PlannerDataContext.Provider value={value}>{children}</PlannerDataContext.Provider>
}

export function usePlannerData(): PlannerData {
  const ctx = useContext(PlannerDataContext)
  if (!ctx) throw new Error('usePlannerData debe usarse dentro de PlannerDataProvider')
  return ctx
}
