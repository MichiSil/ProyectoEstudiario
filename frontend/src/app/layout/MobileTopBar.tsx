import { Link } from 'react-router-dom'

export function MobileTopBar() {
  return (
    <Link
      to="/"
      className="flex items-center gap-2.5 px-5 pt-[max(env(safe-area-inset-top),1.75rem)] pb-3 sm:hidden"
    >
      <img src="/logo-mark.png" alt="" className="h-9 w-9 object-contain" />
      <span className="text-[17px] font-extrabold tracking-tight text-ink">Estudiario</span>
    </Link>
  )
}
