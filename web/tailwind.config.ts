import type { Config } from "tailwindcss";

export default {
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        canvas: "var(--surface-canvas)",
        panel: "var(--surface-panel)",
        raised: "var(--surface-raised)",
        inset: "var(--surface-inset)",
        border: { subtle: "var(--border-subtle)", strong: "var(--border-strong)" },
        ink: { DEFAULT: "var(--text-primary)", secondary: "var(--text-secondary)", faint: "var(--text-faint)" },
        severity: {
          neutral: "var(--severity-neutral)",
          progress: "var(--severity-progress)",
          success: "var(--severity-success)",
          muted: "var(--severity-muted)",
          warning: "var(--severity-warning)",
          critical: "var(--severity-critical)",
        },
      },
      fontFamily: {
        sans: ["ui-sans-serif", "system-ui", "-apple-system", "BlinkMacSystemFont", "Segoe UI", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Monaco", "Consolas", "Liberation Mono", "monospace"],
      },
    },
  },
  plugins: [],
} satisfies Config;
