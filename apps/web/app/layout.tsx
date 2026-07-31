import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "CogniFlow",
  description: "Autonomous Multi-Model AI Orchestration Platform",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
