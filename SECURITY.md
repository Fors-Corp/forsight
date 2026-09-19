# Security Policy

The **source** of this project is public. The **package** (`@fors-corp/forsight`) is not — it's published to GitHub Packages with `restricted` access and installed only by Marc Fors's own repos, so there is no broad public consumer base.

## Reporting a vulnerability

If you find a security issue in this project (e.g. an XSS vector in a component, a dependency with a known CVE), open a private security advisory on this repository ("Security" → "Report a vulnerability") or email developer@marcfors.com directly rather than filing a public issue.

## How this repo is protected

Everything below is enforced by GitHub, not by convention; the full policy (and what to do when a check fails) is in [CONTRIBUTING.md](CONTRIBUTING.md#security--code-quality-policy).

| Control                               | Where                                                                                                                                                                                           |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Secret scanning + push protection     | Repository settings (a pushed credential is rejected before it lands)                                                                                                                           |
| Private vulnerability reporting       | "Security → Report a vulnerability" on this repository                                                                                                                                          |
| Dependabot alerts + security updates  | Repository settings; version updates daily via `.github/dependabot.yml` behind a publish cooldown, auto-merged when green                                                                       |
| `npm audit --audit-level=high` in CI  | `.github/workflows/ci.yml` — a required check on `main`                                                                                                                                         |
| CodeQL (`security-and-quality` suite) | `.github/workflows/codeql.yml` — on every PR, on `main`, weekly; both "Analyze" jobs are required checks (they prove the scan ran; findings are fixed at the source per [CLAUDE.md](CLAUDE.md)) |
| Least-privilege `GITHUB_TOKEN`        | Every workflow declares an explicit `permissions:` block                                                                                                                                        |
| Branch protection on `main`           | CI + CodeQL checks required and up to date, linear history, no force-push, admins included (the exact context list, and the two gaps in it, are in [CLAUDE.md](CLAUDE.md))                      |

## Dependencies

Overlay/interactive components depend on [Radix UI](https://www.radix-ui.com/) primitives; styling depends on Tailwind CSS. Runtime dependencies are kept to those primitives plus `cmdk`, `react-day-picker`, `class-variance-authority`, `clsx`, `tailwind-merge` and `tailwindcss-animate`; `react`/`react-dom` are peers.

**Current audit state: 0 findings** (`npm audit`, September 2026). Advisories that only reach devDependencies still get fixed rather than accepted, because the tree is small enough that a stuck transitive can be pinned with an `overrides` entry in `package.json` (as `uuid` is today) without touching what ships in `dist/`.
