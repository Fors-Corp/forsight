/**
 * Guard against the failure mode that has already hit this repo once (#156):
 * a changeset naming a package the workspace does not contain silently kills
 * `changeset version` — and therefore every publish — while CI on `main`
 * stays green, because nothing in the normal gate reads changeset
 * frontmatter. This repo has no `workspaces` key, so the package.json at the
 * root names the one package every changeset must agree with.
 *
 * `changeset status`'s own exit code was rejected as the mechanism: it is
 * not a stable gate on an unknown package name, and its output doesn't name
 * the offending file. This test does both, and runs inside
 * `npm run test:coverage` (the required `verify` job, on both Node 22 and
 * Node 24) with no workflow edit needed.
 *
 * One `it(...)` that loops over every changeset internally, not a generated
 * test per file — a `.changeset/` directory with zero real changesets in it
 * must still produce a suite that runs (and passes trivially), not an empty
 * one that silently reports nothing.
 */
import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, "..");
const changesetDir = join(repoRoot, ".changeset");

const packageJson = JSON.parse(readFileSync(join(repoRoot, "package.json"), "utf8")) as {
  name: string;
};

/** Every `.changeset/*.md` file except README.md (Changesets' own doc, not a changeset). */
function changesetFiles(): string[] {
  return readdirSync(changesetDir)
    .filter((f) => f.endsWith(".md") && f !== "README.md")
    .sort();
}

/**
 * Parses the frontmatter block (between the first two `---` lines) of a
 * changeset file into its `"<package>": major|minor|patch` entries.
 */
function frontmatterEntries(contents: string): { name: string; bump: string }[] {
  const lines = contents.split("\n");
  const entries: { name: string; bump: string }[] = [];
  let dashCount = 0;
  for (const line of lines) {
    if (line.trim() === "---") {
      dashCount++;
      if (dashCount === 2) break;
      continue;
    }
    if (dashCount !== 1) continue;
    const match = /^"([^"]+)":\s*(major|minor|patch)\s*$/.exec(line.trim());
    if (match) entries.push({ name: match[1], bump: match[2] });
  }
  return entries;
}

describe("changeset package names", () => {
  it("every changeset names the package this workspace actually contains", () => {
    const offenders: string[] = [];

    for (const file of changesetFiles()) {
      const contents = readFileSync(join(changesetDir, file), "utf8");
      for (const entry of frontmatterEntries(contents)) {
        if (entry.name !== packageJson.name) {
          offenders.push(`${file}: "${entry.name}"`);
        }
      }
    }

    expect(
      offenders,
      `changeset(s) name a package other than "${packageJson.name}" ` +
        `(package.json's actual name) — this silently kills \`changeset ` +
        `version\` and every publish behind it (#156):\n` +
        offenders.map((o) => `  - ${o}`).join("\n")
    ).toEqual([]);
  });
});
