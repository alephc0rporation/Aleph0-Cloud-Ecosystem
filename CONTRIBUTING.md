# Contributing to Aleph0 Cardinal

Thanks for helping build Cardinal. This guide covers how to get set up, how we work, and what a
pull request needs before it can be merged.

## Getting set up

Follow [Prerequisites](README.md#prerequisites) and [First run](README.md#first-run) in the
README. When `make up && make dev` works and you can log in at http://localhost:5173, you are
ready.

Read [docs/architecture.md](docs/architecture.md) before larger changes: it explains how the
portal, the API and OpenStack fit together.

## Workflow

1. **Open an issue first** for anything bigger than a small fix, so we can agree on the approach
   before you write the code.
2. **Branch from `main`** with a short descriptive name, e.g. `feat/vm-resize` or
   `fix/token-audience`.
3. **Keep pull requests small and focused.** One change per PR is easier to review and to revert.
4. **Fill in the PR description**: what changed, why, and how you tested it.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <summary in the imperative, lower case, no period>
```

- **Types:** `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`, `perf`
- **Scopes:** `api`, `web`, `deploy`, `docs`; leave the scope out for changes across the repo

Example: `feat(api): cap request bodies at 1 MiB`

## Before you open a pull request

- [ ] `make test` passes (Go vet and tests, ESLint, TypeScript check, Vitest).
- [ ] New behaviour has tests; bug fixes have a test that fails without the fix.
- [ ] If you changed the API contract (`api/openapi/`), you ran `make generate` and committed the result.
- [ ] Docs are updated (README, `docs/`) if setup, commands or behaviour changed.
- [ ] No secrets, tokens or real customer data in code, tests or commits.

## Code guidelines

- **The API contract comes first.** `api/openapi/` (split by area) is the source of truth. Change
  it, run `make generate` (it bundles it into `api/openapi.yaml`), then implement the new method
  of `oapi.StrictServerInterface` in `api/internal/handlers`.
- **Never edit generated files by hand:** `api/openapi.yaml`, `api/internal/oapi/`,
  `api/internal/store/db/`, `web/src/api/gen/` and `web/src/routeTree.gen.ts`.
- **Go:** code is formatted with `gofmt`; errors returned to clients use the RFC 9457
  problem format from `api/internal/problem`.
- **TypeScript/React:** ESLint and `tsc` must be clean; use the generated TanStack Query hooks
  instead of calling `fetch` directly.
- **Line endings are LF** everywhere (enforced by `.gitattributes`).

## Reporting bugs

Open a GitHub issue with the steps to reproduce, what you expected, what happened, and relevant
logs (with tokens and passwords removed).

## Reporting security issues

**Do not open a public issue for a vulnerability.** Report it privately through the repository's
**Security** tab → **Report a vulnerability**. We will acknowledge it and keep you informed until
it is fixed.

## License

Cardinal is licensed under the [Apache License 2.0](LICENSE). By submitting a contribution, you
agree that it is licensed under the same terms, as described in section 5 of the license. Make
sure you have the right to submit the code you contribute.
