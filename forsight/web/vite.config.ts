import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import { forsightAntiFlashScript } from "@marcfs31/forsight/theme";

// Applies a stored theme to <html> before React (and its stylesheet) ever
// paints, so a viewer who picked light mode doesn't see a flash of the
// dark default on every load. The Go agent (internal/api) sets no
// Content-Security-Policy for the dashboard's static shell, so an inline
// <script> in <head> is fine here.
function forsightAntiFlashPlugin(): Plugin {
  return {
    name: "forsight-anti-flash",
    transformIndexHtml(html) {
      return html.replace(
        "<head>",
        `<head>\n    <script>${forsightAntiFlashScript({ storageKey: "forsight-theme" })}</script>`
      );
    },
  };
}

// Base "./" so the built assets resolve correctly when served from an
// embedded filesystem at an arbitrary mount point, not just "/".
export default defineConfig({
  base: "./",
  plugins: [react(), forsightAntiFlashPlugin()],
  server: {
    // `npm run dev` here talks to a real `forsight run` process for its data —
    // start that separately (`go run . run`) on the default :8080.
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
