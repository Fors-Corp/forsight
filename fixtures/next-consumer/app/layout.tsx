// Server Component root layout — mirrors the README's Next.js recipe.
// styles.css is imported from JS (not from globals.css) on purpose: that is
// the path a bundler can drop if the package mis-declares `sideEffects`.
import "@fors-corp/forsight/styles.css";
import "./globals.css";
import { forsightAntiFlashScript } from "@fors-corp/forsight/theme";

export const metadata = { title: "Forsight design-system consumer fixture" };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: forsightAntiFlashScript() }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
