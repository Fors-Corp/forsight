---
"@marcfs31/forsight": minor
---

`Tabs.Root`/`List`/`Trigger`/`Panel` and `SidebarHeader`/`SidebarContent`/
`SidebarFooter`/`SidebarNav`/`AppShell`/`AppShellMain` are now built with
`React.forwardRef`, matching every other compound component in the library
(`Card`, `Dialog`, `AlertDialog`, `Command`, `Table`, `Drawer`,
`DropdownMenu`, `Breadcrumb`, `RadioGroup`, `ToggleGroup`) — they were the
last two that weren't. A consumer passing `ref={ref}` to any of them
previously failed to typecheck (`ref` was not part of the declared prop
type), so a TypeScript app could not measure a tab list, scroll a panel into
view, or focus the sidebar's `<nav>` from outside. Each part also now sets
`displayName`, so React DevTools and test-runner error messages name it
instead of showing `Anonymous`.

New named, exported prop types, closing gaps where a type existed in source
but wasn't part of the public API (or, for `DialogContent`/`AlertDialogAction`/
`AlertDialogCancel`/`DrawerContent`, existed only as an inline, unnamed
intersection): `TabsRootProps`, `TabsTriggerProps`, `TabsPanelProps`,
`TableHeadProps`, `SliderProps`, `DialogContentProps`,
`AlertDialogActionProps`, `AlertDialogCancelProps`, `DrawerContentProps`. A
new test (`src/__tests__/prop-exports.test.ts`) asserts every exported
`*Props` type defined in `src/components/*.tsx` is re-exported from
`src/index.ts` unless explicitly allow-listed, so a future gap like this is
a decision instead of an oversight.
