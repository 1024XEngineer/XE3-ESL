import type { Metadata } from "next";
import Link from "next/link";
import BrandMark from "../BrandMark";
import BrandWordmark from "../BrandWordmark";
import ChangelogContent from "./ChangelogContent";

export const metadata: Metadata = {
  title: "更新日志 · SpeakUp",
  description: "查看 SpeakUp 已正式发布的功能、体验优化与问题修复。",
};

export default function ChangelogPage() {
  return (
    <main className="release-site release-home changelog-page" id="top">
      <nav className="site-nav release-nav release-nav-simple" aria-label="主导航">
        <Link className="brand" href="/" aria-label="SpeakUp 首页">
          <BrandMark />
          <BrandWordmark />
        </Link>
        <div className="nav-links">
          <Link href="/">产品首页</Link>
          <Link href="/changelog" aria-current="page">
            更新日志
          </Link>
        </div>
      </nav>

      <header className="changelog-hero">
        <Link className="changelog-back" href="/">
          ← 返回产品首页
        </Link>
        <p className="eyebrow">产品更新</p>
        <h1>更新日志</h1>
        <p className="changelog-intro">
          记录 SpeakUp 已正式发布的功能、体验优化与问题修复。
        </p>
      </header>

      <ChangelogContent />

      <footer>
        <Link className="brand" href="/" aria-label="SpeakUp 首页">
          <BrandMark />
          <BrandWordmark />
        </Link>
        <p>只记录已经正式上线、与你使用直接相关的变化。</p>
        <span className="release-footer-meta">
          <Link href="/changelog" aria-current="page">
            更新日志
          </Link>
          <i aria-hidden="true">·</i>© 2026 SpeakUp
        </span>
      </footer>
    </main>
  );
}
