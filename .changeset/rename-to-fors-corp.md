---
"@fors-corp/forsight": major
---

The package is now published as `@fors-corp/forsight`. Consumers must change the dependency name in `package.json`, update the `@marcfs31:registry` line in their `.npmrc` to `@fors-corp:registry`, and update every import specifier — including the `/theme`, `/chart`, `/styles.css`, `/tailwind.css`, `/fonts.css` and `/tailwind-preset` subpaths — to the new scope. Nothing about the API, the exports map, or the design tokens changes: this is a rename, not a rewrite. `@marcfs31/forsight` is frozen at 4.2.0 and receives no further releases.
