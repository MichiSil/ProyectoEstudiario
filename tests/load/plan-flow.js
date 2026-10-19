// Prueba de carga del flujo principal de planner-service (ADR-013).
//
// Cada iteración es un estudiante nuevo que: crea una materia, declara su
// disponibilidad, carga un examen, pide una propuesta, la confirma y reintenta
// la confirmación con la misma Idempotency-Key (RN-14).
//
//   docker run --rm -i -e BASE_URL=http://host.docker.internal:8082 grafana/k6 run - < tests/load/plan-flow.js
import http from 'k6/http';
import { check, fail } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:8082';

export const options = {
  scenarios: {
    rampa: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 20 },
        { duration: '1m', target: 50 },
        { duration: '1m', target: 100 },
        { duration: '20s', target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:confirmar}': ['p(95)<500'],
    'http_req_duration{name:proponer}': ['p(95)<500'],
  },
};

const flujo = new Trend('flujo_completo_ms', true);

export default function () {
  const student = `lt-${__VU}-${__ITER}-${Date.now()}`;
  const headers = { 'Content-Type': 'application/json', 'X-User-Id': student };
  const t0 = Date.now();

  let r = http.post(`${BASE}/subjects`, JSON.stringify({ name: 'Análisis II', difficulty: 3 }), { headers, tags: { name: 'materia' } });
  if (!check(r, { 'materia 201': (x) => x.status === 201 })) fail('materia');
  const subjectId = r.json('id');

  r = http.put(`${BASE}/availability`, JSON.stringify({
    weeklySlots: [0, 1, 2, 3, 4, 5, 6].map((d) => ({ weekday: d, startTime: '18:00', endTime: '21:00' })),
    maxDailyMinutes: 120, preferredSessionMinutes: 60, timezone: 'America/Argentina/Cordoba',
  }), { headers, tags: { name: 'disponibilidad' } });
  check(r, { 'disponibilidad 204': (x) => x.status === 204 });

  const examDate = new Date(Date.now() + 14 * 24 * 3600 * 1000).toISOString().slice(0, 10);
  r = http.post(`${BASE}/exams`, JSON.stringify({
    subjectId, type: 'parcial', startsAt: `${examDate}T09:00:00-03:00`, topics: ['Integrales', 'Series', 'Límites'],
  }), { headers, tags: { name: 'examen' } });
  if (!check(r, { 'examen 201': (x) => x.status === 201 })) fail('examen');
  const examId = r.json('id');

  r = http.post(`${BASE}/exams/${examId}/proposals`, null, { headers, tags: { name: 'proponer' } });
  if (!check(r, { 'propuesta 201': (x) => x.status === 201 })) fail('propuesta');
  const planId = r.json('id');

  const confirmHeaders = Object.assign({ 'Idempotency-Key': `k-${student}` }, headers);
  r = http.post(`${BASE}/plans/${planId}/confirm`, null, { headers: confirmHeaders, tags: { name: 'confirmar' } });
  check(r, { 'confirmación 200': (x) => x.status === 200 });
  const sessions = r.json('sessions.#');

  r = http.post(`${BASE}/plans/${planId}/confirm`, null, { headers: confirmHeaders, tags: { name: 'reintento' } });
  check(r, {
    'reintento 200': (x) => x.status === 200,
    'reintento sin duplicar': (x) => x.json('sessions.#') === sessions,
  });

  flujo.add(Date.now() - t0);
}
