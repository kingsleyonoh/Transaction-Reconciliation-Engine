# Transaction Reconciliation Engine — Coding Standards

> Part 1 of 3. Also loaded: `CODING_STANDARDS_TESTING.md`, `CODING_STANDARDS_DOMAIN.md`

These rules are ALWAYS ACTIVE. Follow them on every response without being asked.

## Workflow Pipeline Awareness
- After completing ANY workflow, **read `.agent/workflows/PIPELINE.md`** and suggest the NEXT logical workflow based on the current context.
- **PIPELINE.md is the single source of truth** for "what comes next." Individual workflows do NOT hardcode their next step — they defer to PIPELINE.md.
- Never leave the user guessing what to do next. Always end with a clear next step.
- **When creating a NEW workflow file**, ALWAYS add it to `PIPELINE.md` with its "When Done, Suggest" message.
- **When deleting a workflow file**, ALWAYS remove it from `PIPELINE.md`.
- `PIPELINE.md` must ALWAYS match the actual files in `.agent/workflows/`. If they're out of sync, fix `PIPELINE.md` immediately.

## Domain-Specific Rules

If your task touches any of the domains below, **also read the corresponding rules file before starting**.

| When working on... | Also read |
|--------------------|-----------| 
| Authentication / API keys | `.agent/rules/auth_rules.md` (if exists) |
| Database / migrations / queries | `.agent/rules/db_rules.md` (if exists) |
| Background jobs / scheduling | `.agent/rules/jobs_rules.md` (if exists) |
| API endpoints / validation | `.agent/rules/api_rules.md` (if exists) |

> These files are created by `/bootstrap` when a domain has 5+ concentrated conventions. If a file doesn't exist for a domain, the relevant rules are here in CODING_STANDARDS.md.

## Project-Specific Architecture Rules

### Package Dependency Hierarchy (PRD Section 9)

```
domain     → nothing (pure types)
repository → domain
engine     → domain, repository
adapter    → domain
report     → domain, repository
scheduler  → adapter, engine
api        → engine, adapter, report, repository, domain
cmd/recon  → api, scheduler, config
```

**Rule:** Packages import DOWN only. `domain` never imports from `repository`. `engine` never imports from `api`.

### Go Conventions
- **Go 1.22** — use latest features (`for range` integer, `slices` package, etc.)
- **No ORM** — raw SQL with `sqlx` for scanning. All queries in repository files.
- **Money is BIGINT cents** — NEVER use float/decimal for money. Amount stored as int64 in Go, BIGINT in PostgreSQL.
- **Currency codes** — CHAR(3) ISO 4217. Validate on ingestion.
- **UUIDs everywhere** — `google/uuid` for generation. All primary keys are UUID.
- **Error wrapping** — use `fmt.Errorf("context: %w", err)` for stack context. Return errors up, never swallow.
- **Structured logging** — `zerolog` JSON to stdout. Include `request_id`, `source_id`, `run_id` in context.
- **Interfaces in consumer package** — define interfaces where they're used, not where they're implemented.
- **Table-driven tests** — use `[]struct{ name string; ... }` pattern for test cases.
- **`http.Client` injection** — all adapters accept an `http.Client` parameter for test mock injection.
- **Context propagation** — pass `context.Context` as first parameter to all functions that do I/O.

### Database Conventions
- All timestamps are `TIMESTAMPTZ` (UTC).
- All tables have `created_at` and `updated_at` DEFAULT NOW().
- Deduplication key: `sha256(source_id + external_id)` stored in `dedup_key` column.
- Use `ON CONFLICT DO NOTHING` for idempotent inserts.
- Partial indexes for hot queries (unmatched transactions, open discrepancies).

### API Conventions
- All routes under `/api/v1/` prefix.
- Authentication: `X-API-Key` header, SHA-256 hash comparison.
- Pagination: offset-based `?page=1&per_page=25`, max 100.
- Error format: `{ "error": { "code": "...", "message": "...", "details": [...] } }`.
- Standard error codes: `VALIDATION_ERROR`, `NOT_FOUND`, `DUPLICATE`, `UNAUTHORIZED`, `RATE_LIMITED`, `INTERNAL_ERROR`, `RECONCILIATION_LOCKED`.

## Skill Selection & Orchestration
You have a vast library of specialized skills available. **Use them proactively** — don't wing it when a skill exists for the task.

### How Skill Selection Works
1. **Before starting any implementation task**, mentally scan your available skills for matches.
2. If a relevant skill exists, **read its SKILL.md first** using `view_file`, then follow its guidance.
3. **Announce your choice**: *"I am invoking the [skill-name] skill to ensure this follows best practices."*
4. When multiple skills could apply, invoke the most specific one (e.g., `go-concurrency-patterns` over `clean-code` for a goroutine task).
5. **When in doubt, invoke the skill.** Reading a SKILL.md costs 30 seconds. Getting it wrong costs hours.

### When to Invoke Skills (Non-Negotiable)
- **Building with Go patterns** → find the matching skill (`go-concurrency-patterns`, `clean-code`, etc.)
- **Touching security** (auth, input validation, secrets, API exposure) → invoke a security skill
- **Writing tests** → invoke the testing skill for your language/framework
- **Designing a database schema or API** → invoke the design/architecture skill
- **Debugging a bug** → invoke `systematic-debugging` before guessing
- **Deploying or containerizing** → invoke the deployment skill for your platform
- **Integrating an external API** → check for a dedicated skill first
- **Working with AI/LLM features** → invoke the relevant AI skill (RAG, agents, prompts)
- **Writing documentation** → invoke the documentation skill for the format you need
- **Unfamiliar domain or new library** → research skill first, then build

### What NOT to Do
- ❌ Skip skills because "I already know this" — the skill may have guardrails you'd miss
- ❌ Hardcode patterns from memory when a skill has the latest best practices
- ❌ Use a generic approach when a project-specific skill exists

## Git Commit Convention

**Format:** `type(scope): descriptive message`

| Type | When to use |
|------|------------|
| `feat` | New feature or functionality |
| `fix` | Bug fix |
| `refactor` | Code restructuring without behavior change |
| `test` | Adding or updating tests |
| `docs` | Documentation changes |
| `chore` | Tooling, workflows, config, dependencies |
| `style` | Formatting, whitespace, no logic change |

**Scope** = the module or package affected:
`domain`, `repository`, `engine`, `adapter`, `api`, `report`, `scheduler`, `config`, `migrations`, `cli`, `docker`, `workflows`

**Rules:**
- Subject line max 72 characters.
- Use imperative mood: "add filter" not "added filter".
- Reference the `[BUG]`/`[FIX]`/`[FEATURE]` from `progress.md` when applicable.
- One commit per completed item. Don't bundle unrelated changes.

**Examples:**
```
feat(engine): implement 4-pass matching cascade
fix(engine): handle refund sign reversal in matching
test(engine): add reconciliation cascade integration tests
chore(docker): create multi-stage Dockerfile
```

## AI Discipline Rules (Prevent Common AI Failures)

### No Scope Creep
- **ONLY implement what's asked or what's next in `docs/progress.md`.** Do not add features, helpers, utilities, or "nice-to-haves" that aren't in the spec.
- If you think something SHOULD be added, ASK the user first. Never add it silently.

### No Phantom Dependencies
- **NEVER import a package that isn't in `go.mod`.** Add it FIRST with `go get`, then use it.
- Before using any library method, **verify it exists** in that version. Don't hallucinate API methods.

### No Placeholder Code
- **NEVER write `// TODO`, empty function bodies, or `panic("not implemented")` as final code.** Every function must be fully implemented before marking the task done.

### No Hallucinated APIs
- Before calling any external library method, **verify the method exists** by checking docs or the installed package.
- If unsure, say so and check rather than assuming.

### No Silent Failures
- **NEVER write code that swallows errors silently.** Every `if err != nil` block must return, log, or propagate.
- `_ = someFunc()` that returns an error is permanently BANNED (unless explicitly documented as safe to ignore).

### No Over-Engineering
- Match the spec's complexity level. No abstractions without 2+ concrete implementations.
- Build for the scale defined in the spec (~2,000 transactions/day, 10 concurrent users), not 100x that.

### Verify Before Claiming
- **NEVER say "done" or "all tests pass" without actually running `go test ./...`** and showing the output.
- **NEVER say "this follows the spec" without having read the relevant PRD section** in this session.

### Full Read Rule (CRITICAL — Prevents Context Loss)
- **When ANY workflow instructs you to "read" a file, you MUST read the ENTIRE file from first line to last line.**
- If the file is longer than your read limit, make multiple sequential read calls (e.g., lines 1–200, 201–400, 401–end) until **every line has been read.**
- Do NOT read a partial subset and assume you understand the rest. Critical rules, patterns, and constraints are often buried later in the file.
- This applies universally to: PRD, `progress.md`, `CODING_STANDARDS.md`, `CODEBASE_CONTEXT.md`, Shared Foundation files, source files referenced in tasks, and any other file a workflow tells you to read.

### Read Shared Foundation Before Coding (CRITICAL — Prevents Duplication)
- Before writing ANY new utility, helper, middleware, handler, or shared pattern, read every file listed in the **Shared Foundation** table in `CODEBASE_CONTEXT.md`.
- If a pattern, function, or module already exists there — **USE IT.** Do not recreate it.

### Workflow Discipline
- **Max 25 workflow files** in `.agent/workflows/`. If approaching 25, retire rarely-used workflows.
- Before creating a new workflow, check if an existing one can be extended.

### Search Before Creating (CRITICAL — Prevents Duplicate Code)
- **Before creating ANY new file, function, struct, or utility**, search the codebase first:
  1. `grep_search` for the function/struct name
  2. `find_by_name` for the file name
  3. Check relevant package exports
- If it already exists, **USE IT**. Do not recreate it.
- If a similar function exists, **extend it** — don't create a parallel version.
- When in doubt, **ASK the user**: "I can't find X — does it exist, or should I create it?"

### Use Skills When Available (Skills > Pre-trained Knowledge)
- Before implementing any task, scan your available skills list for domain matches.
- If a matching skill exists (e.g., database → `postgresql`, auth → `auth-implementation-patterns`, Go concurrency → `go-concurrency-patterns`), read its `SKILL.md` and follow its instructions.
- **CRITICAL:** The patterns, architectures, and rules defined in a `SKILL.md` STRICTLY OVERRIDE your general pre-trained knowledge. Always choose the skill's approach over what you "think you know."
- **Always announce:** *"Using skill: [skill-name] for this task."* so the user knows which patterns are being applied.
- If no skill matches, proceed normally.

## File Size Limits
- **Max 300 lines** per source file. If approaching 250, plan to split.
- **Max 50 lines** per function/method.
- **Max 200 lines** per struct methods combined.

## PowerShell Environment
- Use `;` to chain commands, **NEVER** `&&`
- **NEVER use inline `go run -e "..."`** or complex one-liners for multi-step operations. Write a `.go` file or script instead.
- Special characters that break PowerShell: `|`, `>`, `<`, `$`, `()`, `{}`
- Write scripts to files instead of inline commands when possible.

## Git Branching Strategy

### Two-Branch Model
- **`main`** — Production only. Code merges here when ready to deploy.
- **`dev`** — Active development. All work happens here.
- `/implement-next` always runs on `dev`.
- Tests always run against local dev services on `dev` branch.
- Merge `dev` → `main` only when all tests pass and feature is complete.
