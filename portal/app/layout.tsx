import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "SpeakUp · 有记忆的 AI Agent 口语老师",
  description: "主动了解你的目标，陪你准备、模拟和复盘真实世界里的英语沟通，越用越懂你。",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
