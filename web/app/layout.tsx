import type { Metadata } from "next";

import { getEnv } from "@/lib/env";

import "./globals.css";
import { Providers } from "./providers";

export const metadata: Metadata = {
  title: "payd dashboard",
  description: "Operator dashboard for payd",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className="dark">
      <body><Providers tronscanBaseUrl={getEnv().TRONSCAN_BASE_URL}>{children}</Providers></body>
    </html>
  );
}
