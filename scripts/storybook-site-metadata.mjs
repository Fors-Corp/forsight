#!/usr/bin/env node
// Writes the two files that make the published Storybook self-describing:
//
//   storybook-static/build-info.json       what this site was built from
//   storybook-static/versions/index.html   what else is published here
//
// build-info.json is not decoration. deploy-storybook.yml reads it back off
// the live site to decide whether main has moved past what is published - the
// check that heals a deploy missed because the merge was made with
// GITHUB_TOKEN and so raised no push event. The site's own provenance is what
// keeps it current, and "what am I looking at?" becomes answerable from the
// site rather than from the Actions log.
//
//   node scripts/storybook-site-metadata.mjs v4.1.1 v4.1.0
//
// Arguments are the released versions that were built into v/<tag>/. Env:
// GITHUB_SHA, GITHUB_REF_NAME, GITHUB_RUN_ID (all optional off CI).

import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const OUT = process.env.STORYBOOK_OUT ?? "storybook-static";
const versions = process.argv.slice(2).filter(Boolean);

const pkg = JSON.parse(readFileSync("package.json", "utf8"));

/** Last commit date, or null outside a git checkout. */
function committedAt() {
  try {
    return execFileSync("git", ["log", "-1", "--format=%cI"], {
      encoding: "utf8",
    }).trim();
  } catch {
    return null;
  }
}

/** Current commit, preferring what CI says it built. */
function sha() {
  if (process.env.GITHUB_SHA) return process.env.GITHUB_SHA;
  try {
    return execFileSync("git", ["rev-parse", "HEAD"], {
      encoding: "utf8",
    }).trim();
  } catch {
    return "unknown";
  }
}

const info = {
  sha: sha(),
  ref: process.env.GITHUB_REF_NAME ?? "main",
  package: pkg.name,
  package_version: pkg.version,
  committed_at: committedAt(),
  run: process.env.GITHUB_RUN_ID ?? null,
  versions,
};

mkdirSync(join(OUT, "versions"), { recursive: true });
writeFileSync(join(OUT, "build-info.json"), `${JSON.stringify(info, null, 2)}\n`);

const esc = (s) =>
  String(s).replace(
    /[&<>"']/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]
  );

const items = versions.length
  ? versions.map((v) => `<li><a href="../v/${esc(v)}/">${esc(v)}</a></li>`).join("")
  : "<li>No released version is published yet.</li>";

writeFileSync(
  join(OUT, "versions", "index.html"),
  `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark">
<title>Forsight design system · docs versions</title>
<style>
  body{margin:0;padding:40px;background:#0b0d10;color:#e6e9ee;
       font:15px/1.6 -apple-system,"Segoe UI",Helvetica,Arial,sans-serif}
  h1{font-size:20px;margin:0 0 4px}
  .s{color:#aab3c1;font-size:13px;max-width:70ch}
  ul{list-style:none;padding:0;margin:20px 0}
  li{margin:7px 0}
  a{color:#7fd6c8}
  code{font:12.5px ui-monospace,SFMono-Regular,Menlo,monospace;color:#aab3c1}
</style></head><body>
<h1>${esc(pkg.name)} · docs</h1>
<div class="s">Storybook for the design system in <code>src/</code>. The
observability agent is a separate artifact on the <code>forsight-vX.Y.Z</code>
tags and is not documented here.</div>
<ul>
  <li><a href="../">main</a> — current development, <code>${esc(info.sha.slice(0, 7))}</code></li>
  ${items}
</ul>
<div class="s">Built from <code>${esc(info.sha.slice(0, 7))}</code>${
    info.committed_at ? `, committed ${esc(info.committed_at)}` : ""
  }. Machine-readable: <a href="../build-info.json">build-info.json</a>.</div>
</body></html>
`
);

console.log(
  `build-info.json: ${info.sha.slice(0, 7)} (${info.package}@${info.package_version}); versions: ${
    versions.join(" ") || "none"
  }`
);
