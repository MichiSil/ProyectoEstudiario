// Sonda de la jornada de fallas (docs/POSTMORTEM.md, ADR-010).
//
// Manda tráfico parejo por el api-gateway, como lo haría el frontend, y escribe
// una línea por pedido (hora, pedido, status, code, duración e instancia que
// atendió). Con eso se arma la línea de tiempo de cada caída y se cuenta
// cuántos pedidos fallaron, sin depender de mirar el tablero en el momento.
//
// Al empezar registra (si no existen) un estudiante y un administrador, inicia
// sesión con los dos y deja dos resúmenes aprobados del estudiante.
//
//   mkdir -p salida && cp tests/fallas/sonda.js salida/
//   docker run -d --name sonda --network estudiario_default -v "$PWD/salida:/salida" \
//     grafana/k6:0.54.0 run --quiet --log-format raw --log-output file=/salida/sonda.csv \
//     -e DURATION=45m /salida/sonda.js
//   docker stop sonda    # para cortarla antes
//
// Corre desacoplada (-d) para que siga aunque se cierre la terminal. En Git Bash
// de Windows, anteponé MSYS_NO_PATHCONV=1 para que no cambie las rutas /salida.
//
// 10 pedidos por segundo de un mismo usuario superan el control de tráfico del
// gateway (120 por minuto): levantalo con RATE_LIMIT_USER_PER_MIN=6000.
import http from 'k6/http';
import exec from 'k6/execution';
import { fail } from 'k6';

const G = __ENV.BASE_URL || 'http://api-gateway:8080';
const STUDENT = __ENV.STUDENT_EMAIL || 'sonda@estudiario.local';
const ADMIN = __ENV.ADMIN_EMAIL || 'admin@estudiario.local';
const PASSWORD = __ENV.PASSWORD || 'secreta123';
const PDF = '%PDF-1.4\n% resumen de la jornada de fallas\n';

export const options = {
  setupTimeout: '2m',
  scenarios: {
    sonda: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.RATE || 10),
      timeUnit: '1s',
      duration: __ENV.DURATION || '40m',
      preAllocatedVUs: 20,
      maxVUs: 100,
    },
  },
};

function login(name, email) {
  const json = { headers: { 'Content-Type': 'application/json' } };
  http.post(`${G}/api/users/register`, JSON.stringify({ name, email, password: PASSWORD }), json); // 409 si ya existe
  const r = http.post(`${G}/api/users/login`, JSON.stringify({ email, password: PASSWORD }), json);
  if (r.status !== 200) fail(`no se pudo iniciar sesión con ${email}: ${r.status} ${r.body}`);
  return r.json('token');
}

const bearer = (token) => ({ Authorization: `Bearer ${token}` });

export function setup() {
  const student = login('Sonda de Fallas', STUDENT);
  const admin = login('Admin', ADMIN);
  const ids = [];
  const mine = http.get(`${G}/api/forum/summaries/mine?size=50`, { headers: bearer(student) });
  for (const s of mine.json('items') || []) if (s.status === 'aprobado') ids.push(s.id);
  for (let i = ids.length; i < 2; i++) {
    const r = http.post(`${G}/api/forum/summaries`, {
      title: `Resumen de la jornada de fallas ${i + 1}`,
      category: 'Arquitectura de Software',
      file: http.file(PDF, `jornada-${i + 1}.pdf`, 'application/pdf'),
    }, { headers: bearer(student) });
    if (r.status !== 201) fail(`no se pudo publicar: ${r.status} ${r.body}`);
    const a = http.post(`${G}/api/forum/admin/summaries/${r.json('id')}/approve`, null, { headers: bearer(admin) });
    if (a.status !== 200) fail(`no se pudo aprobar: ${a.status} ${a.body}`);
    ids.push(r.json('id'));
  }
  console.log('hora,pedido,status,code,ms,instancia');
  return { student, id: ids[0] };
}

// Cada pedido de la lista sale una vez por vuelta. El perfil del Club va una
// sola vez porque, con el Club caído, cada consulta espera su timeout.
const PEDIDOS = [
  ['listado', (d) => `${G}/api/forum/summaries?page=1&size=20`],
  ['detalle', (d) => `${G}/api/forum/summaries/${d.id}`],
  ['busqueda', (d) => `${G}/api/forum/summaries?q=jornada`],
  ['mis-resumenes', (d) => `${G}/api/forum/summaries/mine`],
  ['sesiones', (d) => `${G}/api/planner/sessions`],
  ['perfil', (d) => `${G}/api/users/me`],
  ['listado', (d) => `${G}/api/forum/summaries?page=1&size=20`],
  ['detalle', (d) => `${G}/api/forum/summaries/${d.id}`],
  ['sesiones', (d) => `${G}/api/planner/sessions`],
  ['club', (d) => `${G}/api/users/me/club`],
];

export default function (data) {
  const n = exec.scenario.iterationInTest;
  const [name, url] = PEDIDOS[n % PEDIDOS.length];
  const r = http.get(url(data), {
    headers: Object.assign({ 'X-Correlation-Id': `sonda-${n}` }, bearer(data.student)),
    tags: { name },
    timeout: '15s',
  });
  let code = '';
  if (r.status === 0) code = r.error_code ? `k6-${r.error_code}` : 'sin-respuesta';
  else if (r.status >= 400) { try { code = r.json('code') || ''; } catch (_) { code = ''; } }
  else if (name === 'club') { try { code = r.json('warning') || ''; } catch (_) { code = ''; } }
  console.log([new Date().toISOString(), name, r.status, code, Math.round(r.timings.duration), r.headers['X-Served-By'] || ''].join(','));
}
