// Prueba de carga del sistema completo (ADR-013): el flujo principal del plan
// de estudio y la lectura del foro al mismo tiempo, por el api-gateway, como
// los usa el frontend.
//
// Dos grupos de usuarios virtuales corren a la vez:
//
//   - plan: cada iteración es un estudiante nuevo que crea una materia, declara
//     su disponibilidad, carga un examen, pide una propuesta, la confirma y
//     reintenta la confirmación con la misma Idempotency-Key (como plan-flow.js);
//   - foro: listado y detalle de los resúmenes aprobados, con algunas
//     calificaciones (como forum-read.js), con la caché prendida.
//
// A diferencia de plan-flow.js y forum-read.js, acá cada pedido pasa por el
// gateway: valida el JWT, aplica el control de tráfico y, en el foro, reparte
// entre las dos instancias. Los JWT se firman acá con el JWT_SECRET del .env,
// igual que el comando devtoken, para no cargar el registro y el login (que
// tienen bcrypt y un límite de 20 pedidos por minuto por IP).
//
// El control de tráfico por usuario (120 por minuto) hay que subirlo, porque
// cada usuario virtual del foro es un solo usuario que lee sin parar:
//
//   RATE_LIMIT_USER_PER_MIN=1000000 docker compose up -d api-gateway
//   docker run --rm -i --network estudiario_default \
//     -e JWT_SECRET=$(grep ^JWT_SECRET= .env | cut -d= -f2 | tr -d '\r') \
//     -e PLAN_VUS=50 -e FORO_VUS=50 \
//     grafana/k6:0.54.0 run - < tests/load/sistema-completo.js
import http from 'k6/http';
import crypto from 'k6/crypto';
import encoding from 'k6/encoding';
import { check, fail, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const G = __ENV.BASE_URL || 'http://api-gateway:8080';
const JWT_SECRET = __ENV.JWT_SECRET;
const PLAN_VUS = Number(__ENV.PLAN_VUS || 50);
const FORO_VUS = Number(__ENV.FORO_VUS || 50);
const RESUMENES = Number(__ENV.RESUMENES || 40);
const MATERIAS = ['Análisis II', 'Física I', 'Química General', 'Álgebra Lineal'];
const PDF = '%PDF-1.4\n% resumen de prueba de carga\n';

// La misma rampa que plan-flow.js: 20 %, 50 % y 100 % de los usuarios.
function rampa(vus) {
  return [
    { duration: '30s', target: Math.ceil(vus * 0.2) },
    { duration: '1m', target: Math.ceil(vus * 0.5) },
    { duration: '1m', target: vus },
    { duration: '20s', target: 0 },
  ];
}

export const options = {
  setupTimeout: '3m',
  scenarios: {
    plan: { executor: 'ramping-vus', exec: 'plan', startVUs: 0, stages: rampa(PLAN_VUS) },
    foro: { executor: 'ramping-vus', exec: 'foro', startVUs: 0, stages: rampa(FORO_VUS) },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:confirmar}': ['p(95)<500'],
    'http_req_duration{name:proponer}': ['p(95)<500'],
    'http_req_duration{name:listado}': ['p(95)<500'],
    'http_req_duration{name:detalle}': ['p(95)<500'],
    // Sin umbral: sólo para que el resumen muestre la latencia de cada uno.
    'http_req_duration{name:materia}': [],
    'http_req_duration{name:disponibilidad}': [],
    'http_req_duration{name:examen}': [],
    'http_req_duration{name:reintento}': [],
    'http_req_duration{name:calificar}': [],
  },
};

const flujo = new Trend('flujo_plan_ms', true);

// JWT HS256 con el formato que valida el gateway (internal/auth): emisor
// users-service, sub = id del usuario y rol.
function token(sub, role) {
  const b64 = (o) => encoding.b64encode(JSON.stringify(o), 'rawurl');
  const now = Math.floor(Date.now() / 1000);
  const body = `${b64({ alg: 'HS256', typ: 'JWT' })}.${b64({ role, name: 'Prueba de carga', iss: 'users-service', sub, iat: now, exp: now + 3600 })}`;
  return `${body}.${crypto.hmac('sha256', JWT_SECRET, body, 'base64rawurl')}`;
}

const auth = (sub, role, extra) => Object.assign({ Authorization: `Bearer ${token(sub, role || 'estudiante')}` }, extra || {});

function publishAndApprove(i, admin) {
  const r = http.post(`${G}/api/forum/summaries`, {
    title: `Resumen de carga ${i}`,
    category: MATERIAS[i % MATERIAS.length],
    file: http.file(PDF, `resumen-${i}.pdf`, 'application/pdf'),
  }, { headers: auth('lt-autor'), tags: { name: 'publicar' } });
  if (!check(r, { 'publicar 201': (x) => x.status === 201 })) return null;
  const id = r.json('id');
  const a = http.post(`${G}/api/forum/admin/summaries/${id}/approve`, null, { headers: admin, tags: { name: 'aprobar' } });
  check(a, { 'aprobar 200': (x) => x.status === 200 });
  return id;
}

export function setup() {
  if (!JWT_SECRET) fail('falta JWT_SECRET (pasalo con -e JWT_SECRET=..., ver el comentario de arriba)');
  const admin = auth('lt-admin', 'admin');
  // Se reusan los resúmenes de una corrida anterior y se publican los que falten.
  const ids = [];
  const mine = http.get(`${G}/api/forum/summaries/mine?size=50`, { headers: auth('lt-autor'), tags: { name: 'preparar' } });
  if (mine.status !== 200) fail(`el foro no respondió por el gateway: ${mine.status} ${mine.body}`);
  for (const s of mine.json('items') || []) {
    if (s.status === 'aprobado' && /^Resumen de carga \d+$/.test(s.title)) ids.push(s.id);
  }
  for (let i = ids.length; i < RESUMENES; i++) {
    const id = publishAndApprove(i, admin);
    if (id) ids.push(id);
  }
  if (ids.length < RESUMENES) fail(`sólo hay ${ids.length} resúmenes aprobados`);
  sleep(15); // el indexador pasa los aprobados a Solr cada 5 s (ADR-006)
  return { ids: ids.slice(0, RESUMENES) };
}

// Flujo principal: un estudiante nuevo por iteración.
export function plan() {
  const student = `lt-${__VU}-${__ITER}-${Date.now()}`;
  const headers = auth(student, 'estudiante', { 'Content-Type': 'application/json' });
  const P = `${G}/api/planner`;
  const t0 = Date.now();

  let r = http.post(`${P}/subjects`, JSON.stringify({ name: 'Análisis II', difficulty: 3 }), { headers, tags: { name: 'materia' } });
  if (!check(r, { 'materia 201': (x) => x.status === 201 })) return;
  const subjectId = r.json('id');

  r = http.put(`${P}/availability`, JSON.stringify({
    weeklySlots: [0, 1, 2, 3, 4, 5, 6].map((d) => ({ weekday: d, startTime: '18:00', endTime: '21:00' })),
    maxDailyMinutes: 120, preferredSessionMinutes: 60, timezone: 'America/Argentina/Cordoba',
  }), { headers, tags: { name: 'disponibilidad' } });
  check(r, { 'disponibilidad 204': (x) => x.status === 204 });

  const examDate = new Date(Date.now() + 14 * 24 * 3600 * 1000).toISOString().slice(0, 10);
  r = http.post(`${P}/exams`, JSON.stringify({
    subjectId, type: 'parcial', startsAt: `${examDate}T09:00:00-03:00`, topics: ['Integrales', 'Series', 'Límites'],
  }), { headers, tags: { name: 'examen' } });
  if (!check(r, { 'examen 201': (x) => x.status === 201 })) return;
  const examId = r.json('id');

  r = http.post(`${P}/exams/${examId}/proposals`, null, { headers, tags: { name: 'proponer' } });
  if (!check(r, { 'propuesta 201': (x) => x.status === 201 })) return;
  const planId = r.json('id');

  const confirmHeaders = Object.assign({ 'Idempotency-Key': `k-${student}` }, headers);
  r = http.post(`${P}/plans/${planId}/confirm`, null, { headers: confirmHeaders, tags: { name: 'confirmar' } });
  check(r, { 'confirmación 200': (x) => x.status === 200 });
  const sessions = r.status === 200 ? r.json('sessions.#') : -1;

  r = http.post(`${P}/plans/${planId}/confirm`, null, { headers: confirmHeaders, tags: { name: 'reintento' } });
  check(r, {
    'reintento 200': (x) => x.status === 200,
    'reintento sin duplicar': (x) => x.status === 200 && x.json('sessions.#') === sessions,
  });

  flujo.add(Date.now() - t0);
}

// Los más vistos: el 80 % de los detalles van a los primeros 10.
function popular(ids) {
  if (Math.random() < 0.8) return ids[Math.floor(Math.random() * Math.min(10, ids.length))];
  return ids[Math.floor(Math.random() * ids.length)];
}

// Lectura del foro: la misma mezcla que forum-read.js.
export function foro(data) {
  const user = `lt-lector-${__VU}`;
  const F = `${G}/api/forum`;
  const p = Math.random();
  if (p < 0.55) {
    const page = Math.random() < 0.8 ? 1 : 2;
    let url = `${F}/summaries?page=${page}&size=20`;
    if (Math.random() < 0.5) url += `&category=${encodeURIComponent(MATERIAS[Math.floor(Math.random() * MATERIAS.length)])}`;
    const r = http.get(url, { headers: auth(user), tags: { name: 'listado' } });
    check(r, { 'listado 200': (x) => x.status === 200 });
  } else if (p < 0.97) {
    const r = http.get(`${F}/summaries/${popular(data.ids)}`, { headers: auth(user), tags: { name: 'detalle' } });
    check(r, { 'detalle 200': (x) => x.status === 200 });
  } else {
    const r = http.put(`${F}/summaries/${popular(data.ids)}/rating`, JSON.stringify({ score: 1 + Math.floor(Math.random() * 5) }),
      { headers: auth(user, 'estudiante', { 'Content-Type': 'application/json' }), tags: { name: 'calificar' } });
    check(r, { 'calificar 200': (x) => x.status === 200 });
  }
}
