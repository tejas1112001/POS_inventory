# Load test results

## 2026-11-02, after the speed work (batched checkout writes, slim POS list, 2 receipt renderers)

2026-11-02 14:34 UTC · 50 tenants · 1000 SKUs each · 60 s · 50 concurrent clients · pause 1s between a client's requests · 50 requests/s · app GOMAXPROCS=4 · host 8 CPUs · Postgres 18 in a container on the same host · setup 32s

| Scenario | Requests | Errors | p50 | p95 | p99 | Target p95 | Met |
|---|---|---|---|---|---|---|---|
| GET /api/dashboard (today) | 600 | 0 | 1.1ms | 249.9ms | 295.4ms | 150ms | no |
| GET /api/products (page of 25, search) | 600 | 0 | 13.7ms | 26.2ms | 31.3ms | 80ms | yes |
| GET /api/products/pos (1000 SKUs) | 600 | 0 | 66.7ms | 158.6ms | 211ms | 120ms | no |
| GET /api/sales/{id}/receipt (PDF download) | 572 | 0 | 9.1ms | 18.2ms | 23.2ms | 30ms | yes |
| POST /api/sales (5 lines) | 600 | 0 | 30ms | 117.2ms | 169.7ms | 60ms | no |

Concurrent checkouts on one variant with 25 in stock: 40 requests at once → 25 sold, 15 refused, 0 left (expected 25 / 15 / 0).

## 2026-11-02, before the speed work (same machine, same day)

2026-11-02 14:28 UTC · 50 tenants · 1000 SKUs each · 60 s · 50 concurrent clients · pause 1s between a client's requests · 49 requests/s · app GOMAXPROCS=4 · host 8 CPUs · Postgres 18 in a container on the same host · setup 32s

| Scenario | Requests | Errors | p50 | p95 | p99 | Target p95 | Met |
|---|---|---|---|---|---|---|---|
| GET /api/dashboard (today) | 600 | 0 | 1ms | 246.8ms | 359ms | 150ms | no |
| GET /api/products (page of 25, search) | 600 | 0 | 12.8ms | 23.6ms | 35.4ms | 80ms | yes |
| GET /api/products/pos (1000 SKUs) | 600 | 0 | 75.2ms | 227.8ms | 294.2ms | 120ms | no |
| GET /api/sales/{id}/receipt (PDF download) | 554 | 0 | 9.3ms | 16.8ms | 20.4ms | 30ms | yes |
| POST /api/sales (5 lines) | 600 | 0 | 36.9ms | 147.7ms | 183.4ms | 60ms | no |

Concurrent checkouts on one variant with 25 in stock: 40 requests at once → 25 sold, 15 refused, 0 left (expected 25 / 15 / 0).

## 2026-11-01 (earlier runs)

### Realistic run (1 s pause per client, dashboard cache on)

2026-11-01 17:51 UTC · 50 tenants · 1000 SKUs each · 60 s · 50 concurrent clients · pause 1s between a client's requests · 50 requests/s · app GOMAXPROCS=4 · host 8 CPUs · Postgres 18 in a container on the same host · setup 23s

| Scenario | Requests | Errors | p50 | p95 | p99 | Target p95 | Met |
|---|---|---|---|---|---|---|---|
| GET /api/dashboard (today) | 600 | 0 | 800µs | 143.1ms | 188ms | 150ms | yes |
| GET /api/products (page of 25, search) | 600 | 0 | 7.4ms | 18.6ms | 23.4ms | 80ms | yes |
| GET /api/products/pos (1000 SKUs) | 600 | 0 | 74.6ms | 166.9ms | 205.3ms | 120ms | no |
| GET /api/sales/{id}/receipt (PDF download) | 578 | 0 | 4.8ms | 13.8ms | 15.3ms | 30ms | yes |
| POST /api/sales (5 lines) | 600 | 0 | 27.9ms | 105.2ms | 156.2ms | 60ms | no |

Concurrent checkouts on one variant with 25 in stock: 40 requests at once → 25 sold, 15 refused, 0 left (expected 25 / 15 / 0).

### Saturation run (no pause: throughput), before the dashboard cache

2026-11-01 17:27 UTC · 50 tenants · 1000 SKUs each · 60 s · 50 concurrent clients · app GOMAXPROCS=4 · host 8 CPUs · Postgres 18 in a container on the same host · setup 24s

| Scenario | Requests | Errors | p50 | p95 | p99 | Target p95 | Met |
|---|---|---|---|---|---|---|---|
| GET /api/dashboard (today) | 3330 | 0 | 259.6ms | 406.5ms | 457.9ms | 150ms | no |
| GET /api/products (page of 25, search) | 3335 | 0 | 82.4ms | 151.2ms | 183.2ms | 80ms | no |
| GET /api/products/pos (1000 SKUs) | 3349 | 0 | 173.5ms | 253.4ms | 288.1ms | 120ms | no |
| GET /api/sales/{id}/receipt (PDF download) | 3314 | 0 | 202.4ms | 311.1ms | 349.1ms | 30ms | no |
| POST /api/sales (5 lines) | 3354 | 0 | 158ms | 274.9ms | 317.3ms | 60ms | no |

Concurrent checkouts on one variant with 25 in stock: 40 requests at once → 25 sold, 15 refused, 0 left (expected 25 / 15 / 0).
