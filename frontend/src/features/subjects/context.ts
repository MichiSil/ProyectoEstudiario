import { useOutletContext } from 'react-router-dom'
import type { Subject } from '@/api/types'

export function useSubjectContext(): { subject: Subject } {
  return useOutletContext<{ subject: Subject }>()
}
