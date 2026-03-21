# Transaction Reconciliation Engine — Coding Standards: Domain & Production

> Part 3 of 3. Also loaded: `CODING_STANDARDS.md`, `CODING_STANDARDS_TESTING.md`

## Deployment Platform

### Default: DigitalOcean VPS (Portfolio / Personal Projects)
- **All portfolio projects deploy to DigitalOcean VPS** via Docker + Traefik reverse proxy.
- Every project ships with `Dockerfile`, `docker-compose.prod.yml`, and `.dockerignore`.
- Traefik auto-routes `project-slug.kingsleyonoh.com` with free SSL via Let's Encrypt.
- Deploy: `ssh` into VPS → `git pull` → `docker compose -f docker-compose.prod.yml up -d`.
- This enables flat hosting cost (~$6/mo total) regardless of project count.

### Client / Production Projects
- **Railway** is the default for client-facing production deployments.
- Railway supports Node.js, Python, Go, Docker, PostgreSQL, Redis — covers most stacks.
- Use `railway up` CLI or Railway's GitHub integration for deployment.

### Frontend Exception
- **Vercel** is an option for frontend-only deployments (Next.js, React SPAs).
- If the project is full-stack, one platform hosts both frontend and backend.
- If the user prefers Vercel for the frontend → split: Vercel (frontend) + Railway/DigitalOcean (backend).

### Client Override
- If the PRD specifies a different platform (AWS, Render, GCP, Azure), use that instead.
- The `/setup-ci` workflow reads this section to determine deployment targets.

### Deployment Files (Every Project)
- `Dockerfile` — multi-stage build (builder → production). Bootstrap customizes base image and build commands per stack.
- `docker-compose.prod.yml` — Traefik labels for automatic subdomain routing + SSL. Bootstrap replaces `PROJECT_SLUG` with actual project name.
- `.dockerignore` — keeps images lean (excludes docs, .agent, node_modules, .git).

## Public Demo Security

Every deployed project MUST implement these protections. The VPS costs money — unprotected endpoints waste resources and invite abuse, regardless of whether the project uses AI or not.

| # | Layer | Rule | Applies to |
|---|-------|------|------------|
| 1 | **Rate Limiting** | Per-IP throttle: 60 req/min for general endpoints, 5 req/day for expensive operations (AI inference, heavy compute, external API calls). | All deployed projects |
| 2 | **Usage Caps** | When daily cap is hit → return HTTP 429 with body: *"Demo limit reached. Contact kingsley@kingsleyonoh.com for full access."* | All deployed projects |
| 3 | **API Key Proxy** | All external API calls MUST go through the backend. Keys live in `.env` only — never in client bundles, never in API responses, never in logs. | Projects with paid APIs |
| 4 | **Input Validation** | Set max input length per endpoint. Sanitize all user input before processing. Reject bad input before it costs money or CPU. | All deployed projects |
| 5 | **Response Sanitization** | Strip internal details from error responses. No stack traces, no system prompts, no file paths, no internal IPs in production error responses. | All deployed projects |
| 6 | **Source Protection** | Minified builds only. No source maps in production. Add `X-Robots-Tag: noai` header on API endpoints. No Swagger/OpenAPI docs exposed in production demo. | All deployed projects |
| 7 | **Demo Banner** | Add to the project README: *"🔒 This is a live demo with rate limits (5 requests/day). Contact kingsley@kingsleyonoh.com for full access."* | Spec (portfolio) projects |
| 8 | **CORS Lock** | CORS origin limited to the demo domain. Never use wildcard `*` in production. | All deployed projects |

### Resource Limits
- `docker-compose.prod.yml` must set `deploy.resources.limits` (default: 512M RAM, 0.5 CPU).
- Bootstrap reminds: keep limits tight. The VPS is shared across all projects.

### When `DEMO_MODE` Applies
- If the project uses paid external APIs (OpenRouter, OpenAI, Stripe test keys, etc.), bootstrap adds `DEMO_MODE=true` to `.env.example`.
- Application code should check `DEMO_MODE` to enforce stricter limits on expensive operations.
- When `DEMO_MODE=true`: rate limits are tighter, usage caps are lower, verbose error messages are suppressed.

## Environment Variables

### `.env.example` Is the Source of Truth
- **`.env.example`** defines the **structure and shape** of all environment variables the project needs.
- **`.env`** contains real secrets and is gitignored — the AI MUST NEVER attempt to read it.
- When you need to know what env vars the project uses → read `.env.example`.
- When adding a new env var → add it to `.env.example` first (with a placeholder value), then document it in `CODEBASE_CONTEXT.md`.
- Bootstrap generates `.env.example` from the PRD. `/sync-context` keeps `CODEBASE_CONTEXT.md` in sync with it.

## Production-Readiness Rules

### Every External Call MUST Handle Failure
- Assume external APIs WILL fail. Every call needs: timeout, retry (with backoff), error logging.
- NEVER silently skip a failed operation.

### Jobs MUST Be Idempotent
- Any scheduled/background job can run twice without causing damage.
- Use `ON CONFLICT DO NOTHING` patterns, never blind inserts.

### Validate ALL Input
- Never trust data from external sources. Validate types, ranges, and required fields.
- If validation fails, log and skip the record — don't crash the whole job.

### Log Everything With Context
- Every operation must include relevant context (request_id, source_id, run_id) in log output.
- Use zerolog structured logging with JSON output.

### No Data Loss
- Never DELETE records in production flows. Use status fields (`status: "archived"`).
- Audit logs are append-only. Never update or delete reconciliation run records.

## Server-Side Performance Rules

### Deduplicate Expensive Calls
If multiple functions on the same request path call the same expensive operation (auth check, config fetch, external API), extract it into a shared cached helper (e.g., request-scoped cache, singleton per request). Never let each function create its own call — N actions × M calls = latency multiplication.

### Parallel by Default
Independent operations (DB queries, API calls, file reads) MUST run concurrently (goroutines + `errgroup`, `sync.WaitGroup`, etc.). Sequential execution is only for data-dependent chains where one result feeds the next.

### Wire It or Delete It
If you create a utility, middleware, or proxy file, connect it to the framework entry point in the same commit. Unwired code creates false confidence — the feature "exists" but doesn't execute.

### Compound Load Audit
After implementing 5+ operations callable from a single entry point (API endpoint, CLI command), audit total I/O calls. Features built incrementally work in isolation but compound into latency regressions that correctness tests never catch.

### Prefer Joins Over Multiple Queries
If sqlx supports the query, use SQL JOINs and eager loading. N separate queries for N related tables is a sequential waterfall — one joined query is one round-trip. This includes any pattern where you fetch IDs from one table then loop to fetch details from another.

### Pin Compute to Data Region
Serverless functions must run in the same region as the database. Unmatched regions add 50-100ms per query. Set this in deployment config during Phase 0 setup — not after performance problems surface.

## Code Structure Rules

### Thin Entry Points
Route handlers, CLI commands, and event handlers must stay thin — validate input, call a service/domain function, format the response. Extract business logic, side effects (notifications, logging, external calls), and data access into a separate layer. Entry points that mix multiple concerns become unmaintainable and untestable.

### Single State Mechanism Per Feature
Multi-step flows must use ONE state management approach. Mixing persistence mechanisms (e.g., database + in-memory cache + context values + background sync) creates maintenance burden and race conditions. Pick one, stick with it.

### Modularity Awareness
Before adding code to any file, assess its current structure. Files should have a single clear responsibility. When a file's scope grows to cover multiple concerns, split by responsibility into separate modules — don't wait for a modularity audit. The project's limits (250 lines/file, 40 lines/function from `/check-modularity`) are guardrails, not targets.
