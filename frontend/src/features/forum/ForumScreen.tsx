import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import clsx from 'clsx'
import { useSearchParams } from 'react-router-dom'
import { MessagesSquare, Search, Trash2, Upload, X } from 'lucide-react'
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
import { OWN_DELETE_QUESTION, SummaryRow } from './shared'
import { UploadSummarySheet } from './UploadSummarySheet'

type Tab = 'explorar' | 'mias' | 'moderacion'

/** Foro de resúmenes (sección 7 del SPEC). La moderación sólo la ven los admins. */
export function ForumScreen() {
  const { user } = useSession()
  const isAdmin = user?.role === 'admin'
  const [searchParams] = useSearchParams()
  const [tab, setTab] = useState<Tab>(() => (isAdmin && searchParams.get('tab') === 'moderacion' ? 'moderacion' : 'explorar'))
  const [uploadOpen, setUploadOpen] = useState(false)
  const [selected, setSelected] = useState<Summary | null>(null)
  const [pendingCount, setPendingCount] = useState<number | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)
  // Calificar no reinicia la pestaña Explorar (perdería los filtros): le avisa
  // que vuelva a leer el listado con la búsqueda que tiene puesta.
  const [ratedSignal, setRatedSignal] = useState(0)
  // Los ids de mis resúmenes, para ofrecer "Eliminar" también en el detalle
  // que se abre desde Explorar (RF-31), y los que se eliminaron en esta
  // pantalla, que se sacan de las listas sin volver a pedirlas.
  const [mineIds, setMineIds] = useState<ReadonlySet<string>>(new Set())
  const [deletedIds, setDeletedIds] = useState<ReadonlySet<string>>(new Set())

  useEffect(() => {
    forumApi
      .mine()
      .then((p) => setMineIds(new Set(p.items.map((s) => s.id))))
      .catch(() => setMineIds(new Set()))
  }, [refreshKey])

  // El número de pendientes en la pestaña, aunque el admin esté en otra.
  useEffect(() => {
    if (!isAdmin) return
    forumApi
      .pending()
      .then((p) => setPendingCount(p.total))
      .catch(() => setPendingCount(null))
  }, [isAdmin, refreshKey])

  function deleted(id: string) {
    setDeletedIds((prev) => new Set(prev).add(id))
    setSelected((s) => (s?.id === id ? null : s))
    if (isAdmin) {
      forumApi
        .pending()
        .then((p) => setPendingCount(p.total))
        .catch(() => setPendingCount(null))
    }
  }

  function replace(updated: Summary) {
    setSelected((s) => (s?.id === updated.id ? updated : s))
    setRefreshKey((k) => k + 1)
    setRatedSignal((k) => k + 1)
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
        {tab === 'explorar' && <ExploreTab onOpen={setSelected} reloadSignal={ratedSignal} hidden={deletedIds} />}
        {tab === 'mias' && (
          <MineTab key={refreshKey} onOpen={setSelected} onUpload={() => setUploadOpen(true)} hidden={deletedIds} onDeleted={deleted} />
        )}
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
        isOwn={!!selected && mineIds.has(selected.id)}
        onClose={() => setSelected(null)}
        onChanged={replace}
        onDeleted={deleted}
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

/** Cuánto se espera para releer el listado después de calificar: el indexador
 * pasa la calificación a Solr cada 5 s (ADR-006). */
const INDEX_DELAY_MS = 6000

/** Búsqueda de aprobados con texto, materia y orden (RF-19, RF-20). */
function ExploreTab({
  onOpen,
  reloadSignal,
  hidden,
}: {
  onOpen: (s: Summary) => void
  reloadSignal: number
  hidden: ReadonlySet<string>
}) {
  const [text, setText] = useState('')
  const [category, setCategory] = useState('')
  const [sort, setSort] = useState<SummarySort>('relevance')
  const [query, setQuery] = useState({ q: '', category: '', sort: 'relevance' as SummarySort, minRating: 0 })
  const [items, setItems] = useState<Summary[] | null>(null)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [error, setError] = useState<unknown>(null)

  const load = useCallback(async (q: typeof query, p: number) => {
    setError(null)
    try {
      const res = await forumApi.search({ q: q.q, category: q.category, sort: q.sort, minRating: q.minRating || undefined, page: p })
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

  // Después de calificar se relee el listado con los mismos filtros: enseguida
  // (el promedio del resumen) y otra vez cuando la calificación ya llegó a
  // Solr, para que el filtro por calificación mínima la tenga en cuenta.
  // Se lee la búsqueda puesta en ese momento: si el estudiante cambia el
  // filtro mientras tanto, se relee con el filtro nuevo.
  const [indexing, setIndexing] = useState(false)
  const queryRef = useRef(query)
  queryRef.current = query
  useEffect(() => {
    if (!reloadSignal) return
    setPage(1)
    void load(queryRef.current, 1)
    setIndexing(true)
    const timer = setTimeout(() => {
      setIndexing(false)
      setPage(1)
      void load(queryRef.current, 1)
    }, INDEX_DELAY_MS)
    return () => clearTimeout(timer)
  }, [reloadSignal, load])

  function submit(e: FormEvent) {
    e.preventDefault()
    setQuery((prev) => ({ ...prev, q: text.trim(), category: category.trim(), sort }))
  }

  // La búsqueda se aplica al apretar "Buscar". Si el estudiante vacía los dos
  // campos, se vuelve solo al listado completo, sin tener que buscar de nuevo.
  function onTextChange(value: string) {
    setText(value)
    if (!value.trim() && !category.trim() && query.q) setQuery((q) => ({ ...q, q: '', category: '' }))
  }
  function onCategoryChange(value: string) {
    setCategory(value)
    if (!value.trim() && !text.trim() && query.category) setQuery((q) => ({ ...q, q: '', category: '' }))
  }
  function clearSearch() {
    setText('')
    setCategory('')
    setQuery((q) => ({ ...q, q: '', category: '', minRating: 0 }))
  }

  const filtering = !!query.q || !!query.category || query.minRating > 0
  const visible = items?.filter((s) => !hidden.has(s.id)) ?? null
  const shownTotal = total - ((items?.length ?? 0) - (visible?.length ?? 0))

  return (
    <div>
      <form onSubmit={submit} className="flex flex-col gap-2">
        <div className="relative">
          <Search size={16} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-subtle" />
          <Input
            type="search"
            value={text}
            onChange={(e) => onTextChange(e.target.value)}
            placeholder="Buscar por título o tema"
            className="pl-9"
          />
        </div>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          <Input value={category} onChange={(e) => onCategoryChange(e.target.value)} placeholder="Materia" />
          {/* RF-20: calificación mínima. Se aplica al elegirla, sin apretar "Buscar". */}
          <Select
            value={query.minRating}
            onChange={(e) => setQuery((q) => ({ ...q, minRating: Number(e.target.value) }))}
            aria-label="Calificación mínima"
          >
            <option value={0}>Cualquier calificación</option>
            <option value={3}>3 ★ o más</option>
            <option value={4}>4 ★ o más</option>
            <option value={4.5}>4,5 ★ o más</option>
          </Select>
          <Select value={sort} onChange={(e) => setSort(e.target.value as SummarySort)} aria-label="Ordenar">
            <option value="relevance">Más relevantes</option>
            <option value="rating">Mejor calificados</option>
            <option value="date">Más nuevos</option>
          </Select>
        </div>
        <div className="flex gap-2">
          <Button type="submit" variant="secondary" size="sm">
            <Search size={14} />
            Buscar
          </Button>
          {filtering && (
            <Button type="button" variant="ghost" size="sm" onClick={clearSearch}>
              <X size={14} />
              Limpiar búsqueda
            </Button>
          )}
        </div>
      </form>

      <div className="mt-4 flex flex-col gap-2">
        {!!error && <ApiErrorNotice error={error} />}
        {indexing && query.minRating > 0 && (
          <p className="animate-fade-in rounded-lg bg-accent-soft px-3 py-2 text-xs text-accent-ink">
            Actualizando el listado con tu calificación: el filtro la tiene en cuenta en unos segundos.
          </p>
        )}
        {visible === null ? (
          <p className="py-8 text-center text-sm text-muted">Cargando resúmenes…</p>
        ) : !visible.length ? (
          <EmptyState
            icon={<MessagesSquare size={28} />}
            title={filtering ? 'No encontramos resúmenes con esa búsqueda' : 'Todavía no hay resúmenes publicados'}
            description={filtering ? 'Probá con otras palabras, sin filtrar por materia o con una calificación mínima más baja.' : 'Subí el primero: lo revisa un admin y queda para todos.'}
            action={
              filtering ? (
                <Button size="sm" variant="secondary" onClick={clearSearch}>
                  Ver todos los resúmenes
                </Button>
              ) : undefined
            }
          />
        ) : (
          <>
            <p className="text-xs text-muted">
              {shownTotal} {shownTotal === 1 ? 'resumen' : 'resúmenes'}
            </p>
            {visible.map((s) => (
              <SummaryRow key={s.id} summary={s} onClick={() => onOpen(s)} />
            ))}
            {(items?.length ?? 0) < total && (
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

/** Mis publicaciones con su estado y, si fue rechazada, el motivo (RF-15). El
 * autor puede eliminar cualquiera, en cualquier estado (RF-31). */
function MineTab({
  onOpen,
  onUpload,
  hidden,
  onDeleted,
}: {
  onOpen: (s: Summary) => void
  onUpload: () => void
  hidden: ReadonlySet<string>
  onDeleted: (id: string) => void
}) {
  const [items, setItems] = useState<Summary[] | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<{ id: string; err: unknown } | null>(null)

  async function remove(id: string) {
    setDeleting(id)
    setDeleteError(null)
    try {
      await forumApi.removeOwn(id)
      setConfirming(null)
      onDeleted(id)
    } catch (err) {
      setDeleteError({ id, err })
    } finally {
      setDeleting(null)
    }
  }

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
  const visible = items.filter((s) => !hidden.has(s.id))

  return (
    <div className="flex flex-col gap-2">
      {!!error && <ApiErrorNotice error={error} />}
      {!visible.length ? (
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
        visible.map((s) => (
          <SummaryRow key={s.id} summary={s} showStatus onClick={() => onOpen(s)}>
            {confirming === s.id ? (
              <div className="mt-3 rounded-lg bg-danger/10 p-3">
                <p className="mb-2 text-xs text-ink">{OWN_DELETE_QUESTION}</p>
                <div className="grid grid-cols-2 gap-2">
                  <Button variant="secondary" size="sm" onClick={() => setConfirming(null)} disabled={deleting === s.id}>
                    Volver
                  </Button>
                  <Button variant="danger" size="sm" disabled={deleting === s.id} onClick={() => void remove(s.id)}>
                    {deleting === s.id ? 'Eliminando…' : 'Sí, eliminar'}
                  </Button>
                </div>
              </div>
            ) : (
              <div className="mt-3 flex justify-end">
                <Button variant="ghost" size="sm" className="text-danger!" onClick={() => setConfirming(s.id)}>
                  <Trash2 size={15} />
                  Eliminar
                </Button>
              </div>
            )}
            {deleteError?.id === s.id && (
              <div className="mt-3">
                <ApiErrorNotice error={deleteError.err} />
              </div>
            )}
          </SummaryRow>
        ))
      )}
    </div>
  )
}
