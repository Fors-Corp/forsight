---
name: release
description: >-
  SemVer, Changesets, and git-tagging workflow for the Forsight design system. Use
  when deciding whether a change needs a changeset and which bump type, when
  cutting a version, when tagging a release, or when checking that versions and
  tags are in sync ("make sure everything is properly SemVered and tagged"). The
  invariant: every version that exists in package.json / CHANGELOG.md has a
  matching annotated `vX.Y.Z` git tag on its release commit.
---

# Versioning & releasing the Forsight design system

Managed with [Changesets](https://github.com/changesets/changesets). Published to
GitHub Packages (`@fors-corp/forsight`). Consumers pin a version, so the
version number and the tag history are a contract.

## 1. Does this change need a changeset?

**Yes** — anything a consumer can observe:

- a new component or a new export
- a new, removed, renamed, or retyped prop
- a visual change (token value, spacing, a variant's look)
- a behavior change (keyboard model, ARIA output, default value)
- a bug fix in shipped code

Run `npx changeset`, pick the bump type (below), write a summary — that text
becomes the `CHANGELOG.md` entry — and **commit the generated `.changeset/*.md`
alongside the code**.

**No** — nothing that reaches `dist/`:

- CI config, `.claude/`, `CONTRIBUTING.md`, this repo's own docs
- dev dependencies, test files, Storybook config, tooling scripts
- formatting-only changes

These land as `chore:` / `test:` / `docs:` commits with **no changeset and no
tag**.

## 2. Bump type (SemVer, pre-1.0 rules do not apply — we are past 1.0.0)

| Bump                        | When                                                                                                                                         |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| **patch** (`1.1.0 → 1.1.1`) | bug fix, internal-only refactor with identical output, dependency bump with no API effect                                                    |
| **minor** (`1.1.0 → 1.2.0`) | new component / export / optional prop; additive, backward-compatible behavior; a visual refinement that doesn't break layout expectations   |
| **major** (`1.1.0 → 2.0.0`) | removed or renamed export/prop; changed default behavior or output that could break a consumer; a deliberate restyle consumers must adapt to |

When several pending changesets have different bump types, `changeset version`
applies the highest.

## 3. Cutting a release

**This is automated** (`.github/workflows/release.yml`, `changesets/action`),
triggered when the `CI` workflow completes successfully on `main` (not on the
raw push — `npm run release` only builds and publishes, it doesn't re-run
lint/tests, so this must never fire on a red `main`):

1. Push a commit carrying one or more `.changeset/*.md` files to `main`, CI
   goes green.
2. The `Release` workflow opens (or updates) a **"chore(release): version
   packages"** PR — this IS `changeset version`'s output (bumped
   `package.json`, rewritten `CHANGELOG.md`, changesets consumed), just
   proposed as a PR instead of pushed directly.
3. Review that PR like any other, merge it (squash, per this repo's merge
   setting — the commit message doesn't matter here, the diff does).
4. That merge triggers `Release` again; this time there are no pending
   changesets, so it runs `npm run release` (build + `changeset publish`),
   authenticating to GitHub Packages with the workflow's own `GITHUB_TOKEN`
   (same-repo publish needs no separate PAT/secret), and creates the
   `vX.Y.Z` tag + GitHub Release automatically.

No local steps required for a normal release. **Manual fallback** (CI down,
debugging the action, or a deliberate one-off) — same commands the action
runs, executed locally:

```bash
npx changeset version        # consumes .changeset/*.md → bumps package.json, rewrites CHANGELOG.md
```

Then **verify the bumped state before committing** — run the full gate from the
`testing` skill (`typecheck`, `lint`, `format:check`, `test:coverage`, `build`,
`smoke`, `test:package`, `size-limit`, `build-storybook`, `test:storybook`).

```bash
git add -A
git commit -m "chore(release): <x.y.z>"     # the ONLY thing in this commit is the version bump + CHANGELOG + consumed changeset files
git tag -a v<x.y.z> -m "v<x.y.z> — see CHANGELOG.md"
git push && git push --tags
npm run release   # build + changeset publish — needs a locally-authed npm.pkg.github.com token
```

### Rules for the release commit and tag

- The release commit contains **only** `package.json`, `CHANGELOG.md`, and the
  deleted `.changeset/*.md` files — no code. Code changes belong in the feature
  commits that carried the changesets.
- The tag is **annotated** (`git tag -a`), named exactly `vX.Y.Z`, and points at
  the release commit.
- One tag per version. Never move or delete a published tag; a mistake is fixed
  with the next version, not by rewriting history.
- A `chore`/`test`/`docs` commit is never tagged, even if it sits between a
  feature commit and its release commit — the tag can be several commits after
  the change it versions, which is normal Changesets flow.

## 4. Checking versions and tags are in sync

```bash
node -p "require('./package.json').version"   # current version
grep -E '^## ' CHANGELOG.md | head -5         # every released version has a heading
git tag --list 'v*' --sort=-v:refname          # every version heading has a matching tag
```

Every `## X.Y.Z` heading in `CHANGELOG.md` must have a `vX.Y.Z` tag, and
`package.json`'s `version` must equal the newest heading. If a version was
bumped but never tagged, tag its release commit now:

```bash
git tag -a vX.Y.Z <release-commit-sha> -m "vX.Y.Z — see CHANGELOG.md"
```

## One-time repo setup this depends on (already done, noted for reference)

- `Settings → Actions → General → Workflow permissions` set to **read and
  write** — without it the auto `GITHUB_TOKEN` can't publish packages or open
  the version PR. Check: `gh api repos/Fors-Corp/forsight/actions/permissions/workflow`.
- Branch protection on `main` blocks force-push/deletion but does **not**
  require PRs — the release PR is opened by the action and merged like any
  other, but nothing stops a direct push for anything else.

## History shape (reference)

```
v1.0.0  (tag)  feat: first stable release              ← had changesets
               chore: repo hygiene                     ← no changeset, no tag
               feat: a11y + responsiveness pass        ← had a changeset
               chore: testing infrastructure           ← no changeset, no tag
v1.1.0  (tag)  chore(release): 1.1.0                    ← consumes the changeset above
```
