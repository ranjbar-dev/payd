import type { Metadata } from "next";
import { Fira_Code, Fira_Sans } from "next/font/google";

import { getEnv } from "@/lib/env";

import "./globals.css";
import { Providers } from "./providers";

const firaSans = Fira_Sans({
  variable: "--font-sans",
  display: "swap",
  subsets: ["latin"],
  weight: ["300", "400", "500", "600", "700"],
});

const firaCode = Fira_Code({
  variable: "--font-mono",
  display: "swap",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
});

export const metadata: Metadata = {
  title: "payd dashboard",
  description: "Operator dashboard for payd",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className={`dark ${firaSans.variable} ${firaCode.variable}`}>
      <body><Providers tronscanBaseUrl={getEnv().TRONSCAN_BASE_URL}>{children}</Providers></body>
    </html>
  );
}
