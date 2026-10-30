import { useCallback, useEffect, useRef, useState } from 'react'

export type PomodoroPhase = 'estudio' | 'descanso'

export function usePomodoro(studyMinutes: number, breakMinutes: number) {
  const [phase, setPhase] = useState<PomodoroPhase>('estudio')
  const [secondsLeft, setSecondsLeft] = useState(studyMinutes * 60)
  const [running, setRunning] = useState(false)
  const [completedCycles, setCompletedCycles] = useState(0)
  const intervalRef = useRef<number | null>(null)

  // si cambian los minutos configurados y no está corriendo, reflejarlo de una
  useEffect(() => {
    if (running) return
    setSecondsLeft((phase === 'estudio' ? studyMinutes : breakMinutes) * 60)
  }, [studyMinutes, breakMinutes, phase, running])

  useEffect(() => {
    if (!running) {
      if (intervalRef.current) window.clearInterval(intervalRef.current)
      return
    }
    intervalRef.current = window.setInterval(() => {
      setSecondsLeft((s) => {
        if (s > 1) return s - 1
        // fin de la fase actual: cambia de fase y arranca la siguiente
        setPhase((prevPhase) => {
          const next: PomodoroPhase = prevPhase === 'estudio' ? 'descanso' : 'estudio'
          if (prevPhase === 'estudio') setCompletedCycles((c) => c + 1)
          if (navigator.vibrate) navigator.vibrate(200)
          return next
        })
        return 0
      })
      return
    }, 1000)
    return () => {
      if (intervalRef.current) window.clearInterval(intervalRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [running])

  // cuando secondsLeft llega a 0 mientras corre, recién ahí cargamos la
  // duración de la fase nueva (así el usuario ve "00:00" un instante, como
  // cualquier timer de pomodoro real, y no salta directo al próximo número)
  useEffect(() => {
    if (!running || secondsLeft !== 0) return
    const duration = (phase === 'estudio' ? studyMinutes : breakMinutes) * 60
    const timeout = window.setTimeout(() => setSecondsLeft(duration), 400)
    return () => window.clearTimeout(timeout)
  }, [secondsLeft, running, phase, studyMinutes, breakMinutes])

  const start = useCallback(() => setRunning(true), [])
  const pause = useCallback(() => setRunning(false), [])
  const reset = useCallback(() => {
    setRunning(false)
    setPhase('estudio')
    setSecondsLeft(studyMinutes * 60)
    setCompletedCycles(0)
  }, [studyMinutes])

  return { phase, secondsLeft, running, completedCycles, start, pause, reset }
}
