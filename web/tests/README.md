# Frontend regression tests

Run the existing helper tests with `pnpm test`.

Browser tests exercise the real Next.js UI, React Query mutations and dialogs.
They use isolated browser contexts, synthetic authentication and intercepted API
responses; they require no running Go server or provider credentials.

```sh
pnpm install --frozen-lockfile
pnpm exec playwright install chromium
pnpm test:e2e
```

The test runner starts its own development server on `127.0.0.1:4173`. Keep that
port free. Tests cover site deletion confirmation/cancellation/error recovery,
channel draft isolation, parameter validation, pending save behavior, exact
mutation payloads and settings retry. Unexpected API requests and browser errors
fail the tests. Screenshots and traces are retained under `test-results/` on
failure; inspect a trace with `pnpm exec playwright show-trace <trace.zip>`.

Run `pnpm exec tsc --noEmit`, `pnpm lint` and `pnpm build` for type, lint and
production build validation.
