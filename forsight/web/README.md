# forsight/web

forsight's dashboard — a small React app built on
[`@marcfs31/forsight`](https://github.com/marcfs31/forsight/pkgs/npm/forsight),
the same design system this repo publishes: `StatCard`, `LineChart`,
`StatusDot`, and `Table` render live data from forsight's own `/api/v1/*`
endpoints.

It depends on a pinned published `@marcfs31/forsight` from GitHub Packages
(see `package.json` and `.npmrc`), not a local `file:` link — so the two
artifacts version independently. Authenticate to
`https://npm.pkg.github.com` (a `packages:read` token) before installing:

```bash
npm ci                  # or npm install
npm run dev             # vite dev server; proxies /api to :8080 — run `go run . run` alongside it
npm run build           # production build → dist/
```

You won't usually run these directly — `make build-web` (in `forsight/`)
installs, builds, and copies `dist/` into `../internal/api/webdist/` for
`go:embed`. Run that (or `make build`) after changing anything here or
bumping the design-system pin, and commit the refreshed `webdist/` output
alongside your change — CI's `web` job fails the build otherwise (see
`.github/workflows/forsight-ci.yml`'s diff check). `make build-web` supplies
the `npm ci` token itself — `NODE_AUTH_TOKEN` if set, `gh auth token`
otherwise — so it needs no `.npmrc` edits or exported variables from you.
