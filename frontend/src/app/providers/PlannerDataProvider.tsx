import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { plannerApi } from '@/api/planner'
import { errorMessage } from '@/api/client'
import type { Availability, Exam, Session, Subject } from '@/api/types'
import { readJson, writeJson } from '@/lib/storage'
import { useSession } from './SessionProvider'

/**
 * Datos de planificación del estudiante.
 *
 * - Materias, exámenes y sesiones se leen siempre de planner-service
 *   (GET /subjects, GET /exams y GET /sessions): el backend es la única
 *   fuente de verdad.
 * - planner-service todavía no expone GET /availability. El frontend recuerda
 *   la disponibilidad que guardó con PUT /availability en este navegador. Es
 *   sólo una caché de lo que aceptó el servidor: nunca se usa para validar.
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
      const [subjectList, examList, sessionList] = await Promise.all([
        plannerApi.listSubjects(),
        plannerApi.listExams(),
        plannerApi.listSessions(),
      ])
      setSubjects(subjectList ?? [])
      setExams(examList ?? [])
      setSessions(sessionList ?? [])
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setLoading(false)
    }
  }, [user])

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
      rememberExam,
      rememberAvailability,
    }),
    [subjects, sessions, exams, availability, loading, error, refresh, refreshSessions, addSubject, rememberExam, rememberAvailability],
  )

  return <PlannerDataContext.Provider value={value}>{children}</PlannerDataContext.Provider>
}

export function usePlannerData(): PlannerData {
  const ctx = useContext(PlannerDataContext)
  if (!ctx) throw new Error('usePlannerData debe usarse dentro de PlannerDataProvider')
  return ctx
}
