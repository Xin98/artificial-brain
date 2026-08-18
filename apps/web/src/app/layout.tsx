import type { Metadata } from "next";

import "./globals.css";

export const metadata: Metadata = {
  title: "Artificial Brain",
  description: "AI-native personal workbench",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>): React.JSX.Element {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
