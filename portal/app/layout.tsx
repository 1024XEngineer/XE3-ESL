import type { Metadata } from "next";
import "./globals.css";
import "./marketing.css";

export const metadata: Metadata = {
  title: "SpeakUp · 有记忆的 AI Agent 口语老师",
  description: "围绕真实任务陪你准备、开口和复盘，把重要的英文沟通先练到心里有底。",
  icons: {
    icon: "/assets/brand/speakup-mascot-v2.png",
    apple: "/assets/brand/speakup-mascot-v2.png",
  },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
