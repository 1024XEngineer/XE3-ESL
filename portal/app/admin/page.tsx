import type { Metadata } from "next";
import AdminDashboard from "./AdminDashboard";

export const metadata: Metadata = {
  title: "SpeakUp · 体验申请看板",
  description: "SpeakUp 门户匿名转化与首批体验申请管理。",
};

export default function AdminPage() {
  return <AdminDashboard />;
}
