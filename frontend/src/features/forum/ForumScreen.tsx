import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import clsx from 'clsx'
import { MessagesSquare, Search, Upload } from 'lucide-react'
import { PageHeader } from '@/components/PageHeader'
import { Button } from '@/components/ui/Button'
import { Input, Select } from '@/components/ui/Field'
import { EmptyState } from '@/components/ui/EmptyState'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { forumApi, type SummarySort } from '@/api/forum'
import type { Summary } from '@/api/types'
import { useSession } from '@/app/providers/SessionProvider'
import { ModerationPanel } from './ModerationPanel'
import { SummaryDetailSheet } from './SummaryDetailSheet'
import { SummaryRow } from './shared'
import { UploadSummarySheet } from './UploadSummarySheet'

type Tab = 'explorar' | 'mias' | 'moderacion'

/** Foro de resúmenes (sección 7 del SPEC). La moderación sólo la ven los admins. */
export function ForumScreen() {
  const { user } = useSession()
  const isAdmin = user?.role === 'admin'
  const [tab, setTab] = useState<Tab>('explorar')
  const [uploadOpen, setUploadOpen] = useState(false)
  const [selected, setSelected] = useState<Summary | null>(null)
  const [pendingCount, setPendingCount] = useState<number | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)

  // El número de pendientes en la pestaña, aunque el admin esté en otra.
  useEffect(() => {
    if (!isAdmin) return
    forumApi
      .pending()
      .then((p) => setPendingCount(p.total))
      .catch(() => setPendingCount(null))
  }, [isAdmin, refreshKey])

  function replace(updated: Summary) {
    setSelected((s) => (s?.id === updated.id ? updated : s))
    setRefreshKey((k) => k + 1)
  }

  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <PageHeader
        icon={<MessagesSquare size={20} />}
        title="Foro"
        subtitle="Resúmenes que comparten otros estudiantes."
        action={
          <Button size="sm" onClick={() => setUploadOpen(true)}>
            <Upload size={15} />
            <span className="hidden min-[400px]:inline">Subir resumen</span>
            <span className="min-[400px]:hidden">Subir</span>
          </Button>
        }
      />

      <div className="mt-5 flex gap-1 rounded-xl bg-surface-raised p-1" role="tablist">
        <TabButton active={tab === 'explorar'} onClick={() => setTab('explorar')}>
          Explorar
        </TabButton>
        <TabButton active={tab === 'mias'} onClick={() => setTab('mias')}>
          <span className="min-[400px]:hidden">Míos</span>
          <span className="hidden min-[400px]:inline">Mis resúmenes</span>
        </TabButton>
        {isAdmin && (
          <TabButton active={tab === 'moderacion'} onClick={() => setTab('moderacion')}>
            Moderación
            {!!pendingCount && (
              <span className="ml-1.5 rounded-full bg-danger px-1.5 py-0.5 text-[10px] font-bold leading-none text-white">
                {pendingCount}
              </span>
            )}
          </TabButton>
        )}
      </div>

      <div className="mt-5">
        {tab === 'explorar' && <ExploreTab key={refreshKey} onOpen={setSelected} />}
        {tab === 'mias' && <MineTab key={refreshKey} onOpen={setSelected} onUpload={() => setUploadOpen(true)} />}
        {tab === 'moderacion' && isAdmin && <ModerationPanel onCountChange={setPendingCount} />}
      </div>

      <UploadSummarySheet
        open={uploadOpen}
        onClose={() => setUploadOpen(false)}
        onUploaded={() => {
          setTab('mias')
          setRefreshKey((k) => k + 1)
        }}
      />
      <SummaryDetailSheet
        summary={selected}
        isAdmin={isAdmin}
        onClose={() => setSelected(null)}
        onChanged={replace}
        onDeleted={() => setRefreshKey((k) => k + 1)}
      />
    </div>
  )
}

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={clsx(
        'flex flex-1 items-center justify-center rounded-lg px-2 py-2 text-sm font-medium transition-colors',
        active ? 'bg-surface text-ink shadow-[var(--shadow-sm)]' : 'text-muted hover:text-ink',
      )}
    >
      {children}
    </button>
  )
}

/** Búsqueda de aprobados con texto, materia y orden (RF-19, RF-20). */
function ExploreTab({ onOpen }: { onOpen: (s: Summary) => void }) {
  const [text, setText] = useState('')
  const [category, setCategory] = useState('')
  const [sort, setSort] = useState<SummarySort>('relevance')
  const [query, setQuery] = useState({ q: '', category: '', sort: 'relevance' as SummarySort })
  const [items, setItems] = useState<Summary[] | null>(null)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [error, setError] = useState<unknown>(null)

  const load = useCallback(async (q: typeof query, p: number) => {
    setError(null)
    try {
      const res = await forumApi.search({ q: q.q, category: q.category, sort: q.sort, page: p })
      setItems((prev) => (p === 1 || !prev ? res.items : [...prev, ...res.items]))
      setTotal(res.total)
    } catch (err) {
      setError(err)
      setItems((prev) => prev ?? [])
    }
  }, [])

  useEffect(() => {
    setPage(1)
    void load(query, 1)
  }, [query, load])

  function submit(e: FormEvent) {
    e.preventDefault()
    setQuery({ q: text.trim(), category: category.trim(), sort })
  }

  const filtering = !!query.q || !!query.category

  return (
    <div>
      <form onSubmit={submit} className="flex flex-col gap-2">
        <div className="relative">
          <Search size={16} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-subtle" />
          <Input value={text} onChange={(e) => setText(e.target.value)} placeholder="Buscar por título o tema" className="pl-9" />
        </div>
        <div className="grid grid-cols-2 gap-2">
          <Input value={category} onChange={(e) => setCategory(e.target.value)} placeholder="Materia" />
          <Select value={sort} onChange={(e) => setSort(e.target.value as SummarySort)} aria-label="Ordenar">
            <option value="relevance">Más relevantes</option>
            <option value="rating">Mejor calificados</option>
            <option value="date">Más nuevos</option>
          </Select>
        </div>
        <Button type="submit" variant="secondary" size="sm" className="self-start">
          <Search size={14} />
          Buscar
        </Button>
      </form>

      <div className="mt-4 flex flex-col gap-2">
        {!!error && <ApiErrorNotice error={error} />}
        {items === null ? (
          <p className="py-8 text-center text-sm text-muted">Cargando resúmenes…</p>
        ) : !items.length ? (
          <EmptyState
            icon={<MessagesSquare size={28} />}
            title={filtering ? 'No encontramos resúmenes con esa búsqueda' : 'Todavía no hay resúmenes publicados'}
            description={filtering ? 'Probá con otras palabras o sin filtrar por materia.' : 'Subí el primero: lo revisa un admin y queda para todos.'}
          />
        ) : (
          <>
            <p className="text-xs text-muted">
              {total} {total === 1 ? 'resumen' : 'resúmenes'}
            </p>
            {items.map((s) => (
              <SummaryRow key={s.id} summary={s} onClick={() => onOpen(s)} />
            ))}
            {items.length < total && (
              <Button
                variant="secondary"
                size="sm"
                className="self-center"
                onClick={() => {
                  const next = page + 1
                  setPage(next)
                  void load(query, next)
                }}
              >
                Ver más
              </Button>
            )}
          </>
        )}
      </div>
    </div>
  )
}

/** Mis publicaciones con su estado y, si fue rechazada, el motivo (RF-15). */
function MineTab({ onOpen, onUpload }: { onOpen: (s: Summary) => void; onUpload: () => void }) {
  const [items, setItems] = useState<Summary[] | null>(null)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    forumApi
      .mine()
      .then((p) => setItems(p.items))
      .catch((err) => {
        setError(err)
        setItems([])
      })
  }, [])

  if (items === null) return <p className="py-8 text-center text-sm text-muted">Cargando tus resúmenes…</p>

  return (
    <div className="flex flex-col gap-2">
      {!!error && <ApiErrorNotice error={error} />}
      {!items.length ? (
        <EmptyState
          icon={<Upload size={28} />}
          title="Todavía no subiste resúmenes"
          description="Compartí los tuyos: después de que un admin los revise, quedan para todos."
          action={
            <Button size="sm" onClick={onUpload}>
              <Upload size={15} />
              Subir resumen
            </Button>
          }
        />
      ) : (
        items.map((s) => <SummaryRow key={s.id} summary={s} showStatus onClick={() => onOpen(s)} />)
      )}
    </div>
  )
}
