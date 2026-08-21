"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";

interface Funnel {
  views: number;
  clicks: number;
  submissions: number;
  clickRate: number;
  submitRate: number;
}

interface Signup {
  id: string;
  scenario: string;
  urgency: string;
  target_role: string;
  challenge: string;
  contact: string;
  source: string;
  campaign: string;
  created_at: string;
}

interface Source {
  source: string;
  visits: number;
}

interface Daily {
  day: string;
  views: number;
  clicks: number;
  submissions: number;
}

interface Summary {
  funnel: Funnel;
  recent: Signup[];
  sources: Source[];
  daily: Daily[];
}

function percent(value: number): string {
  return `${(value * 100).toFixed(value > 0 && value < 0.1 ? 1 : 0)}%`;
}

export default function AdminDashboard() {
  const [password, setPassword] = useState("");
  const [summary, setSummary] = useState<Summary | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function loadSummary(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");

    try {
      const response = await fetch("/api/admin/summary", {
        headers: { "x-portal-admin-password": password },
      });
      const result = await response.json().catch(() => ({})) as Summary & {
        error?: string;
      };
      if (!response.ok) {
        setSummary(null);
        setError(result.error || "暂时无法读取数据。");
        return;
      }
      setSummary(result);
    } catch {
      setSummary(null);
      setError("网络连接失败，请稍后重试。");
    } finally {
      setLoading(false);
    }
  }

  async function exportWaitlist() {
    setError("");
    try {
      const response = await fetch("/api/admin/export", {
        headers: { "x-portal-admin-password": password },
      });
      if (!response.ok) {
        setError("导出失败，请重新输入管理密码。");
        return;
      }

      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = "speakup-waitlist.csv";
      link.click();
      URL.revokeObjectURL(url);
    } catch {
      setError("网络连接失败，暂时无法导出。");
    }
  }

  function lockDashboard() {
    setPassword("");
    setSummary(null);
    setError("");
  }

  if (!summary) {
    return (
      <main className="admin-login">
        <section className="admin-login-panel">
          <Link className="brand" href="/" aria-label="返回 SpeakUp 首页">
            <span className="brand-mark" aria-hidden="true">S</span>
            <span>SpeakUp</span>
          </Link>
          <p className="eyebrow">首批体验 · 内部看板</p>
          <h1>看看谁真的<br />想用 SpeakUp。</h1>
          <p>输入管理密码，查看访问、申请点击、表单提交和最近报名。</p>
          <form onSubmit={loadSummary}>
            <label>
              <span>管理密码</span>
              <input
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                autoComplete="current-password"
                minLength={12}
                required
              />
            </label>
            {error && <p className="form-error" role="alert">{error}</p>}
            <button className="button" type="submit" disabled={loading}>
              {loading ? "正在读取…" : "进入数据看板"}
            </button>
          </form>
          <Link className="admin-back-link" href="/">← 返回门户</Link>
        </section>
      </main>
    );
  }

  const maxDaily = Math.max(1, ...summary.daily.map((item) => Number(item.views)));

  return (
    <main className="admin-dashboard">
      <header className="admin-header">
        <Link className="brand" href="/">
          <span className="brand-mark" aria-hidden="true">S</span>
          <span>SpeakUp</span>
        </Link>
        <div>
          <span>首批体验数据</span>
          <button type="button" onClick={lockDashboard}>锁定看板</button>
        </div>
      </header>

      <section className="admin-hero">
        <div>
          <p className="eyebrow">最近 14 天与累计转化</p>
          <h1>用户有没有<br />真的举手？</h1>
        </div>
        <button className="button" type="button" onClick={exportWaitlist}>导出报名 CSV</button>
      </section>

      <section className="funnel-grid" aria-label="转化漏斗">
        <article>
          <span>01 · 到达门户</span>
          <strong>{summary.funnel.views}</strong>
          <small>匿名访客</small>
        </article>
        <article>
          <span>02 · 点击申请</span>
          <strong>{summary.funnel.clicks}</strong>
          <small>{percent(summary.funnel.clickRate)} 到达 → 点击</small>
        </article>
        <article className="funnel-result">
          <span>03 · 提交申请</span>
          <strong>{summary.funnel.submissions}</strong>
          <small>{percent(summary.funnel.submitRate)} 点击 → 提交</small>
        </article>
      </section>

      <section className="admin-insights">
        <article className="trend-card">
          <header>
            <div>
              <span>14 天趋势</span>
              <h2>每日有效访问</h2>
            </div>
            <em>不含静态资源和扫描请求</em>
          </header>
          <div className="trend-chart" aria-label="近十四天访问趋势">
            {summary.daily.length ? summary.daily.map((item) => (
              <div className="trend-column" key={item.day}>
                <div>
                  <i style={{ height: `${Math.max(8, Number(item.views) / maxDaily * 100)}%` }} />
                  <b style={{ height: `${Math.max(4, Number(item.submissions) / maxDaily * 100)}%` }} />
                </div>
                <span>{item.day.slice(5)}</span>
              </div>
            )) : <p className="empty-state">发布内容后，这里会显示每天的真实访问和报名趋势。</p>}
          </div>
        </article>

        <article className="source-card">
          <span>访客来源</span>
          <h2>内容从哪里带来用户</h2>
          <div>
            {summary.sources.length ? summary.sources.map((item) => (
              <p key={item.source}>
                <strong>{item.source}</strong>
                <span>{item.visits} 次访问</span>
              </p>
            )) : <p className="empty-state">UTM 来源数据尚未产生。</p>}
          </div>
        </article>
      </section>

      <section className="signup-section">
        <header>
          <div>
            <p className="eyebrow">最近 50 份申请</p>
            <h2>首批体验名单</h2>
          </div>
          <span>联系方式只在此看板显示</span>
        </header>
        <div className="signup-table-wrap">
          <table>
            <thead>
              <tr>
                <th>提交时间</th>
                <th>场景 / 时间</th>
                <th>目标与卡点</th>
                <th>联系方式</th>
                <th>来源</th>
              </tr>
            </thead>
            <tbody>
              {summary.recent.length ? summary.recent.map((item) => (
                <tr key={item.id}>
                  <td>{new Date(item.created_at).toLocaleString("zh-CN", { hour12: false })}</td>
                  <td><strong>{item.scenario}</strong><small>{item.urgency}</small></td>
                  <td><strong>{item.target_role || "未填写目标"}</strong><small>{item.challenge || "未填写卡点"}</small></td>
                  <td>{item.contact}</td>
                  <td><strong>{item.source || "直接访问"}</strong><small>{item.campaign || "—"}</small></td>
                </tr>
              )) : (
                <tr><td className="empty-table" colSpan={5}>还没有申请。表单提交后会立即出现在这里。</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </main>
  );
}
