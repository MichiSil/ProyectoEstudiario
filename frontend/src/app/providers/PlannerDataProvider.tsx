import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { plannerApi } from '@/api/planner'
import { errorMessage } from '@/api/client'
import type { Availability, Exam, Session, Subject } from '@/api/types'
import { readJson, writeJson } from '@/lib/storage'
import { useSession } from './SessionProvider'

/**
 * Datos de planificación del estudiante.
 *
 * - Materias y sesiones se leen siempre de planner-service (GET /subjects y
 *   GET /sessions): el backend es la única fuente de verdad.
 * - planner-service todavía no expone GET /exams ni GET /availability. Para
 *   poder mostrar el calendario, el frontend recuerda las respuestas de
 *   POST /exams y PUT /availability que recibió en este navegador. Es sólo una
 *   caché de lo que devolvió el servidor: nunca se usa para validar ni se
 *   manda de vuelta como dato propio. Cuando existan esos GET, se reemplaza
 *   por la lectura directa.
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
  rememberExam: (exam: Exam) => void
  rememberAvailability: (availability: Availability) => void
}

const PlannerDataContext = createContext<PlannerData | null>(null)

export function PlannerDataProvider({ children }: { children: ReactNode }) {
  const { user } = useSession()
  const examsKey = `estudiario-exams:${user?.id ?? ''}`
  const availabilityKey = `estudiario-availability:${user?.id ?? ''}`

  const [subjects, setSubjects] = useState<Subject[]>([])
  const [sessions, setSessions] = useState<Session[]>([])
  const [exams, setExams] = useState<Exam[]>(() => readJson(examsKey, []))
  const [availability, setAvailability] = useState<Availability | null>(() => readJson(availabilityKey, null))
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setExams(readJson(examsKey, []))
    setAvailability(readJson(availabilityKey, null))
  }, [examsKey, availabilityKey])

  const refresh = useCallback(async () => {
    if (!user) return
    setError(null)
    try {
      const [subjectList, sessionList] = await Promise.all([plannerApi.listSubjects(), plannerApi.listSessions()])
      setSubjects(subjectList ?? [])
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

  const rememberExam = useCallback(
    (exam: Exam) => {
      setExams((prev) => {
        const next = [...prev.filter((e) => e.id !== exam.id), exam]
        writeJson(examsKey, next)
        return next
      })
    },
    [examsKey],
  )

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
