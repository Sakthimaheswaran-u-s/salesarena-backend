# SalesArena API — Go + Gin + MongoDB + Redis

Backend for the Sales Rewards & Recognition portal. Serves the React app in
`../frontend`, computes points/streaks/leaderboards, and persists everything.

## Stack

| Concern | Choice |
|---|---|
| HTTP | [Gin](https://github.com/gin-gonic/gin) with `gin-contrib/cors` |
| Persistence | **MongoDB** — `users`, `daily_activity` (per-BDA/day rollups), `events` (point ledger) |
| Redis | opaque bearer **sessions** (sliding TTL), **login-streak lock** (`SETNX` so a day's bonus is awarded exactly once even under concurrent logins), and a **versioned read cache** for leaderboard / overview / team responses (30 s TTL, every write bumps `cache:ver`) |
| Passwords | bcrypt |

## Run it

### Option A — Docker (everything)
```bash
cd backend
docker compose up -d --build       # Mongo + Redis + API on :8080
```

### Option B — local Go, Docker only for Mongo + Redis
```bash
cd backend
docker compose up -d mongo redis
cp .env.example .env               # edit if needed
go run ./cmd/server
```

### Option C — local Go with your own Mongo/Redis
Point `MONGO_URI` / `REDIS_ADDR` at them (see `.env.example`) and `go run ./cmd/server`.

On first start, if the `users` collection is empty, the server seeds 60 days
of demo data (13 users, 720 daily rollups, ~1,400 events). The generator is a
bit-for-bit port of the frontend's mock, so numbers match either way.

Then start the frontend (`cd ../frontend && npm run dev`) — Vite proxies
`/api` to `http://localhost:8080`.

```bash
go test ./...      # unit tests (scoring rules, PRNG parity with the JS mock)
```

## Demo accounts

| Role | Email | Password |
|---|---|---|
| BDA | `priya.sharma@salesarena.io` (any of the 12 BDAs) | `bda123` |
| BDM | `rahul.verma@salesarena.io` | `bdm123` |

## Scoring rules (`internal/rules`)

Mirrors `frontend/src/config/rules.js`.

| Event | Points |
|---|---|
| Lead won | +50 |
| Lead dropped | −50 |
| Daily login, streak day *N* | `10 + min((N−1)×5, 50)` |

A streak stays alive through the end of the following day; a missed day resets it.
The user document stores `currentStreak`, `bestStreak`, `lastLoginDate`.

## API

All responses are JSON. Authenticated routes need `Authorization: Bearer <token>`.

| Method | Path | Who | Purpose |
|---|---|---|---|
| POST | `/api/auth/login` | – | `{email, password, role}` → `{user, reward, token}`. For BDAs, records today's login and returns the streak reward. |
| POST | `/api/auth/logout` | any | revoke token |
| GET | `/api/auth/me` | any | current user |
| GET | `/api/leaderboard?range=&q=&team=` | any | ranked BDAs with rank change vs previous period |
| GET | `/api/bda/:id/overview?range=` | self / BDM | dashboard payload (rank, streak, tiles, charts, feed, achievements) |
| GET | `/api/bda/:id/activity` | self / BDM | full points ledger grouped by day |
| POST | `/api/bda/:id/calls` | self / BDM | `{calls, minutes, date?}` — add calls & talk time |
| POST | `/api/bda/:id/leads` | self / BDM | `{outcome: "won"\|"dropped", company?, date?}` — log a lead and its points |
| GET | `/api/team/overview?range=` | BDM | team tiles, 14-day charts, top performers, watchlist, teams |
| GET | `/api/team/daily?date=` | BDM | per-BDA report for one day |
| POST | `/api/demo/reset` | – | wipe + reseed (only when `DEMO_MODE=true`) |
| GET | `/healthz` | – | liveness |

`range` ∈ `today | week | month | all` (default `month`).

Errors: `{"error": "..."}` with 400 (bad input), 401 (bad credentials / no session),
403 (wrong role or not your data), 404, 500.

### Ranking
Net points desc → leads won desc → leads dropped asc → calls desc → name.

## Layout

```
cmd/server/main.go        wiring, seeding, graceful shutdown
internal/config           env + .env loading
internal/rules            scoring constants & streak formula (tests)
internal/model            Mongo documents + API response shapes
internal/store            MongoDB repository (indexes, aggregations)
internal/cache            Redis: sessions, login lock, versioned cache
internal/seed             deterministic demo generator (tests)
internal/auth             bcrypt + token generation
internal/service          business logic: login reward, ranking, overview, team, reports, ingestion
internal/api              Gin router, middleware (auth / role / ownership), handlers
```

## Production notes
- Set `DEMO_MODE=false`, `GIN_MODE=release`, real `CORS_ORIGINS`, and `TZ` to the sales team's zone (streak day boundaries depend on it).
- Put the API behind TLS; tokens are bearer secrets.
- Call and lead ingestion (`/calls`, `/leads`) are meant for the dialer/CRM integration — protect them with a service account (BDM role) or extend the middleware with an API-key check.
