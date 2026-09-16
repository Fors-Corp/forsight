/// <reference types="vite/client" />

// TypeScript 7 checks side-effect imports (main.tsx imports ./index.css); this
// is the reference every Vite app carries so `*.css` and the other asset
// modules Vite serves have declarations.
