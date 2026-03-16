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
