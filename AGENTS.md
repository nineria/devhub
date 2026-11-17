# Dev Hub

One Go binary that supervises local services, runs health checks and serves a responsive page to watch
and control them. Services live in `devhub.json`. The page can add, edit and delete them
(`manage.go`), the only way a request sets a command, so those routes are off unless the hub is
loopback-only or `DEVHUB_EDIT=1`, and they check `X-Devhub`, `Host` and `Origin`.

- Backend: `main.go`, `runner.go` (process supervision), `checker.go` (checks, heartbeats, buttons),
  `logbuf.go` (log ring, levels), `api.go` (HTTP API and log streams), `config.go`, `manage.go` (editing services: validate, write
  the file, swap runners).
- Page: `web/` (React, Tailwind, TanStack Query and ui-sdk, built with Vite+), embedded from `web/dist`.
- Build: `make` (builds the page, then the binary). Check: `make check`.
- Page changes without restarting the hub: run it with `DEVHUB_UI=web/dist`, then `cd web && pnpm run build`.

Before using ui-sdk, follow the ui-sdk skill if it's installed, and read
`web/node_modules/@ui-sdk/react/docs/llms.txt`. To build a new component, also search
`web/node_modules/@ui-sdk/react/docs/core.md` by heading, and read `rendering.md` next to it.
