# Load test results

## Realistic run (1 s pause per client, dashboard cache on)

2026-11-01 17:51 UTC · 50 tenants · 1000 SKUs each · 60 s · 50 concurrent clients · pause 1s between a client's requests · 50 requests/s · app GOMAXPROCS=4 · host 8 CPUs · Postgres 18 in a container on the same host · setup 23s

| Scenario | Requests | Errors | p50 | p95 | p99 | Target p95 | Met |
|---|---|---|---|---|---|---|---|
| GET /api/dashboard (today) | 600 | 0 | 800µs | 143.1ms | 188ms | 150ms | yes |
| GET /api/products (page of 25, search) | 600 | 0 | 7.4ms | 18.6ms | 23.4ms | 80ms | yes |
| GET /api/products/pos (1000 SKUs) | 600 | 0 | 74.6ms | 166.9ms | 205.3ms | 120ms | no |
| GET /api/sales/{id}/receipt (PDF download) | 578 | 0 | 4.8ms | 13.8ms | 15.3ms | 30ms | yes |
| POST /api/sales (5 lines) | 600 | 0 | 27.9ms | 105.2ms | 156.2ms | 60ms | no |

Concurrent checkouts on one variant with 25 in stock: 40 requests at once → 25 sold, 15 refused, 0 left (expected 25 / 15 / 0).

## Saturation run (no pause: throughput), before the dashboard cache

2026-11-01 17:27 UTC · 50 tenants · 1000 SKUs each · 60 s · 50 concurrent clients · app GOMAXPROCS=4 · host 8 CPUs · Postgres 18 in a container on the same host · setup 24s

| Scenario | Requests | Errors | p50 | p95 | p99 | Target p95 | Met |
|---|---|---|---|---|---|---|---|
| GET /api/dashboard (today) | 3330 | 0 | 259.6ms | 406.5ms | 457.9ms | 150ms | no |
| GET /api/products (page of 25, search) | 3335 | 0 | 82.4ms | 151.2ms | 183.2ms | 80ms | no |
| GET /api/products/pos (1000 SKUs) | 3349 | 0 | 173.5ms | 253.4ms | 288.1ms | 120ms | no |
| GET /api/sales/{id}/receipt (PDF download) | 3314 | 0 | 202.4ms | 311.1ms | 349.1ms | 30ms | no |
| POST /api/sales (5 lines) | 3354 | 0 | 158ms | 274.9ms | 317.3ms | 60ms | no |

Concurrent checkouts on one variant with 25 in stock: 40 requests at once → 25 sold, 15 refused, 0 left (expected 25 / 15 / 0).
