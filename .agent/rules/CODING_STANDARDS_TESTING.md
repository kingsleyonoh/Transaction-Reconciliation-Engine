# Transaction Reconciliation Engine — Coding Standards: Testing

> Part 2 of 3. Also loaded: `CODING_STANDARDS.md`, `CODING_STANDARDS_DOMAIN.md`

## Testing Rules — Anti-Cheat (CRITICAL)

### Never Do These
- **NEVER modify a test to make it pass.** Fix the IMPLEMENTATION, not the test.
- **NEVER use empty test bodies.**
- **NEVER hardcode return values** just to satisfy a test.
- **NEVER use broad error handlers** to swallow errors that would make tests fail.
- **NEVER mock the thing being tested.** Only mock external dependencies.
- **NEVER skip or mark tests as expected failures** without explicit user approval.
- **NEVER weaken a test assertion** to make it pass.
- **NEVER delete a failing test.** Failing tests are bugs. Fix them.

### TDD Sequence is Non-Negotiable
- Tests FIRST, then implementation. Never the reverse.
- You MUST create test files BEFORE creating implementation files.
- You MUST run tests and see RED (failures) before writing any implementation.
- You MUST show the RED PHASE EVIDENCE output (as defined in `implement-next.md` Step 5) before proceeding to Green Phase.
- The ONLY exception: `[SETUP]` items (scaffolding, config, infrastructure) where no testable behavior exists yet.
- If you catch yourself implementing without tests — STOP, delete the implementation, write the tests first.

### Always Do These
- **Test BEHAVIOR, not implementation.**
- **Test edge cases:** empty inputs, nil, zero, negative, missing, duplicate.
- **Test sad paths:** API errors, timeouts, invalid data.
- **Assertions must be specific:** `assert.Equal(t, expected, result)`, not `assert.NotNil(t, result)`.
- **Use table-driven tests** for Go: `[]struct{ name string; ... }` pattern.

## Test Quality Checklist (Anti-False-Confidence)

Before moving from RED → GREEN, verify ALL applicable categories have tests:

| # | Category | What to Test |
|---|----------|-------------|
| 1 | Happy path | Does it work with valid, normal input? |
| 2 | Required fields | Does it reject nil/empty for required fields? |
| 3 | Uniqueness | Does it enforce unique constraints (dedup_key)? |
| 4 | Defaults | Do default values apply correctly when field is omitted? |
| 5 | FK relationships | Do foreign keys enforce CASCADE/PROTECT correctly? |
| 6 | Tenant isolation | Can Source A see Source B's data? (if multi-source) |
| 7 | Edge cases | Empty strings, zero, negative, very long strings, special chars |
| 8 | Error paths | What happens when Redis is down, DB is down, input is malformed? |
| 9 | String representation | Does `String()` / `Stringer` return something meaningful? |
| 10 | Meta options | Are ordering, indexes, and constraints working? |

**If a category applies and you skip it, you're cheating.** If RED phase shows fewer than 2 failures, add more tests — you're probably not testing enough.

## Edge Case Coverage Guide

### Models
- Every field from the spec → at least 1 test per constraint
- Every FK → test CASCADE behavior
- Every choice field → test all valid values + 1 invalid value

### Services
- Boundary values (min, max, zero, negative)
- Invalid input types
- Idempotency (running twice = same result)
- Mock external API failures (Stripe, PayPal)

### API Handlers
- Authenticated vs unauthenticated access (X-API-Key header)
- Correct HTTP methods (GET/POST/PUT/DELETE)
- Response format validation (JSON error format)
- Source scoping (source_id isolation)

## Test Modularity Rules
1. **One test file per package** — `foo_test.go` in same package or `foo_integration_test.go` in `tests/`
2. **Max 300 lines per test file** — split if larger
3. **Test setup creates only what that test needs** — no global state
4. **Tests are independent** — no shared state, no ordering dependency
5. **Any single test can run in isolation** — `go test -run TestName ./internal/engine/`
6. **Test names describe business behavior** — `TestReconciler_ExactMatchProducesConfidenceOne`
7. **No test helpers longer than 10 lines** — extract to a `tests/helpers.go` if needed

## Business-Context Testing
- Tests must reflect the BUSINESS PURPOSE described in the spec.
- Every test must answer: Does this protect data? Apply rules correctly? Handle failure? Match the spec?
- Test names must describe business behavior, not technical actions.

## Live Integration Testing (Mock Policy)

### The Rule: Don't Mock What You Own
If you control the service and can run it locally → test against the real thing.

### Service Fallback Hierarchy
When deciding how to test a service, follow this order:
1. **Local instance** (best) — Docker, CLI, emulator on your machine
2. **Cloud dev instance** (good) — dedicated test project / staging environment
3. **Mock** (last resort) — only when options 1 and 2 are impossible

### Test LIVE (Never Mock)
- PostgreSQL database (local Docker) — validates schema, column names, constraints, query behavior
- Redis (local Docker) — validates dedup keys, locks
- Your own API endpoints — call the actual route via `httptest`
- Your own reconciliation engine — test the real function

### Mock ONLY These
- Stripe API calls (use recorded fixtures in `tests/fixtures/stripe/`)
- PayPal API calls (use recorded fixtures in `tests/fixtures/paypal/`)
- Bank file content (use static files in `tests/fixtures/bankfiles/`)
- Sentry error reporting
- Rate-limited external APIs you don't control
- Services with irreversible side effects

### Why This Matters
A mock that returns `{ "source_id": 1 }` will pass even when the real column is `sourceID`. A mock that returns success will pass even when the real constraint rejects your data. Mocks test your ASSUMPTIONS about the service. Live tests test REALITY.

### Test Cleanup
- Each test MUST clean up after itself
- Use database transactions with rollback when possible for speed
