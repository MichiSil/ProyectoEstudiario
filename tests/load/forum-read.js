// Prueba de carga de lectura del foro (ADR-007, 6): mide el listado y el
// detalle de los resúmenes aprobados, con y sin caché.
//
// Al empezar publica y aprueba RESUMENES resúmenes en 4 materias (o reusa los
// de una corrida anterior) y espera a que Solr los indexe. Después, cada usuario virtual pide sobre todo el
// listado (primeras páginas, con y sin filtro por materia) y el detalle de los
// más vistos, con algunas calificaciones y aprobaciones intercaladas para que
// haya invalidaciones.
//
// Le pega directo a forum-service (sin el gateway), con el INTERNAL_TOKEN del
// .env, como plan-flow.js. Se corre dos veces con la misma carga: con
// FORUM_CACHE_PROVIDER=none y con FORUM_CACHE_PROVIDER=memcached en el .env
// (ver RESULTADOS.md):
//
//   docker run --rm -i --network estudiario_default -e BASE_URL=http://forum-service:8083 \
//     -e INTERNAL_TOKEN=$(grep ^INTERNAL_TOKEN= .env | cut -d= -f2 | tr -d '\r') \
//     grafana/k6:0.54.0 run - < tests/load/forum-read.js
import http from 'k6/http';
import { check, fail, sleep } from 'k6';

const BASE = __ENV.BASE_URL || 'http://localhost:8083';
const INTERNAL_TOKEN = __ENV.INTERNAL_TOKEN;
const RESUMENES = Number(__ENV.RESUMENES || 40);
const MATERIAS = ['Análisis II', 'Física I', 'Química General', 'Álgebra Lineal'];
const PDF = '%PDF-1.4\n% resumen de prueba de carga\n';

export const options = {
  setupTimeout: '3m',
  scenarios: {
    lectura: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '20s', target: 50 },
        { duration: '1m30s', target: 50 },
        { duration: '10s', target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:listado}': ['p(95)<500'],
    'http_req_duration{name:detalle}': ['p(95)<500'],
  },
};

function headers(user, extra) {
  return Object.assign({ 'X-Internal-Token': INTERNAL_TOKEN, 'X-User-Id': user, 'X-User-Name': 'Prueba%20de%20carga' }, extra || {});
}

const ADMIN = headers('lt-admin', { 'X-User-Role': 'admin' });

function publishAndApprove(i) {
  const r = http.post(`${BASE}/summaries`, {
    title: `Resumen de carga ${i}`,
    category: MATERIAS[i % MATERIAS.length],
    file: http.file(PDF, `resumen-${i}.pdf`, 'application/pdf'),
  }, { headers: headers('lt-autor'), tags: { name: 'publicar' } });
  if (!check(r, { 'publicar 201': (x) => x.status === 201 })) return null;
  const id = r.json('id');
  const a = http.post(`${BASE}/admin/summaries/${id}/approve`, null, { headers: ADMIN, tags: { name: 'aprobar' } });
  check(a, { 'aprobar 200': (x) => x.status === 200 });
  return id;
}

export function setup() {
  if (!INTERNAL_TOKEN) fail('falta INTERNAL_TOKEN (pasalo con -e INTERNAL_TOKEN=..., ver el comentario de arriba)');
  // Las dos corridas usan los mismos datos: se reusan los resúmenes de una
  // corrida anterior y sólo se publican los que falten.
  const ids = [];
  const mine = http.get(`${BASE}/summaries/mine?size=50`, { headers: headers('lt-autor'), tags: { name: 'preparar' } });
  for (const s of mine.json('items') || []) {
    if (s.status === 'aprobado' && /^Resumen de carga \d+$/.test(s.title)) ids.push(s.id);
  }
  for (let i = ids.length; i < RESUMENES; i++) {
    const id = publishAndApprove(i);
    if (id) ids.push(id);
  }
  if (ids.length < RESUMENES) fail(`sólo hay ${ids.length} resúmenes aprobados`);
  // El indexador sincroniza con Solr cada 5 s y Solr confirma en ≤ 5 s (ADR-006).
  sleep(15);
  return { ids: ids.slice(0, RESUMENES) };
}

// Los más vistos: el 80 % de los detalles van a los primeros 10.
function popular(ids) {
  if (Math.random() < 0.8) return ids[Math.floor(Math.random() * Math.min(10, ids.length))];
  return ids[Math.floor(Math.random() * ids.length)];
}

export default function (data) {
  const user = `lt-${__VU}`;
  const p = Math.random();
  if (p < 0.55) {
    // Listado: casi siempre la primera página, a veces la segunda, con y sin materia.
    const page = Math.random() < 0.8 ? 1 : 2;
    let url = `${BASE}/summaries?page=${page}&size=20`;
    if (Math.random() < 0.5) url += `&category=${encodeURIComponent(MATERIAS[Math.floor(Math.random() * MATERIAS.length)])}`;
    const r = http.get(url, { headers: headers(user), tags: { name: 'listado' } });
    check(r, { 'listado 200': (x) => x.status === 200 });
  } else if (p < 0.97) {
    const r = http.get(`${BASE}/summaries/${popular(data.ids)}`, { headers: headers(user), tags: { name: 'detalle' } });
    check(r, { 'detalle 200': (x) => x.status === 200 });
  } else if (p < 0.999) {
    // Calificar invalida el detalle (ADR-007, 4).
    const r = http.put(`${BASE}/summaries/${popular(data.ids)}/rating`, JSON.stringify({ score: 1 + Math.floor(Math.random() * 5) }),
      { headers: headers(user, { 'Content-Type': 'application/json' }), tags: { name: 'calificar' } });
    check(r, { 'calificar 200': (x) => x.status === 200 });
  } else {
    // Una aprobación nueva (1 de cada 1000 iteraciones) cambia la generación del listado.
    publishAndApprove(`extra ${__VU}-${__ITER}`);
  }
}
