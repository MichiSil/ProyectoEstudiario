/**
 * Momento de marca al abrir la app: se usa como pantalla de carga real
 * (mientras se resuelve la sesión) y, en el primer ingreso de una sesión de
 * navegador, como bienvenida breve — ver AppSplashGate. No es un login.
 */
export function BrandSplash({ tagline = 'Tu espacio de estudio' }: { tagline?: string }) {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-5 bg-paper px-6">
      <div className="animate-fade-in flex flex-col items-center gap-5">
        <img src="/estudiarioimg1.png" alt="" className="h-28 w-28 object-contain" />
        <div className="text-center">
          <p className="text-2xl font-extrabold tracking-tight text-ink">Estudiario</p>
          <p className="mt-1 text-sm text-muted">{tagline}</p>
        </div>
      </div>
    </div>
  )
}
