# Ostler

One Go binary that supervises local services, runs health checks and serves a responsive page to watch
and control them. Services live in `ostler.json`. The page can add, edit and delete them
(`manage.go`), the only way a request sets a command, so those routes are off unless Ostler is
loopback-only or `OSTLER_EDIT=1`, and they check `X-Ostler`, `Host` and `Origin`.

Style: warm stone neutrals with one copper accent and a horseshoe mark (`web/src/Brand.tsx`, `docs/logo.svg`,
`web/public/favicon.svg`). Keep the README and the page in that voice: calm, plain, a stable hand, not a control room.

- Backend: `main.go`, `runner.go` (process supervision), `checker.go` (checks, heartbeats, buttons),
  `logbuf.go` (log ring, levels), `api.go` (HTTP API and log streams), `config.go`, `manage.go` (editing services: validate, write
  the file, swap runners).
- Page: `web/` (React, Tailwind, TanStack Query and ui-sdk, built with Vite+), embedded from `web/dist`.
- Build: `make` (builds the page, then the binary). Check: `make check`.
- Page changes without restarting Ostler: run it with `OSTLER_UI=web/dist`, then `cd web && pnpm run build`.

Before using ui-sdk, follow the ui-sdk skill if it's installed, and read
`web/node_modules/@ui-sdk/react/docs/llms.txt`. To build a new component, also search
`web/node_modules/@ui-sdk/react/docs/core.md` by heading, and read `rendering.md` next to it.

Branches: open pull requests against `develop`. CI passing with no conflict squash-merges them
(`.github/workflows/auto-merge.yml`); `develop` reaches `main` by a pull request merged by hand.
