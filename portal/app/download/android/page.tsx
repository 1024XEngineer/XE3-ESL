import Link from "next/link";
import BrandMark from "../../BrandMark";

const releaseFields = [
  ["版本名称（versionName）", "制品就绪后公开"],
  ["版本号（versionCode）", "制品就绪后公开"],
  ["发布时间", "制品就绪后公开"],
  ["APK 大小", "制品就绪后公开"],
  ["最低 Android 版本", "制品就绪后公开"],
  ["支持的 ABI", "制品就绪后公开"],
];

const supportFields = [
  ["更新状态", "首个正式版本发布后在本页持续更新"],
  ["更新日志", "随每个可下载制品同步公开"],
  ["隐私与权限", "开放下载前公开隐私说明与权限清单"],
  ["问题反馈", "开放下载前公开正式反馈入口"],
];

export default function AndroidDownloadPage() {
  return (
    <main className="release-site download-page" id="top">
      <nav className="site-nav release-nav" aria-label="主导航">
        <Link className="brand" href="/" aria-label="返回 SpeakUp 首页"><BrandMark /><span>SpeakUp</span></Link>
        <div className="nav-links"><Link href="/">产品首页</Link><a href="#verify">验证信息</a></div>
        <Link className="button button-small" href="/">返回首页</Link>
      </nav>

      <header className="download-hero">
        <div className="download-hero-copy">
          <Link className="download-back" href="/">← 返回产品首页</Link>
          <p className="eyebrow">Android 官方版</p><h1>SpeakUp for Android</h1>
          <p>首个公开版本正在准备。当前还没有可公开下载和验证的 APK，完成签名与生产验证后，本页才会开放唯一入口。</p>
          <div className="download-state" role="status"><span><i aria-hidden="true" />准备中</span><small>不会提供空链接、假二维码或未经核验的版本。</small></div>
        </div>
        <aside className="download-availability" aria-labelledby="available-title">
          <p className="eyebrow">开放下载时同时提供</p><h2 id="available-title">文件之外，还有完整的验证信息。</h2>
          <ul><li>版本、时间与兼容范围</li><li>APK 文件 SHA-256</li><li>签名证书 SHA-256</li><li>安装步骤与更新记录</li></ul>
        </aside>
      </header>

      <section className="download-section" aria-labelledby="release-title">
        <div className="download-section-heading"><p className="eyebrow">发布信息</p><h2 id="release-title">拿到文件前，先知道它是什么。</h2></div>
        <dl className="download-facts">{releaseFields.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
      </section>

      <section className="download-section download-verify" id="verify" aria-labelledby="verify-title">
        <div className="download-section-heading"><p className="eyebrow">验证信息</p><h2 id="verify-title">文件校验与签名校验，是两件不同的事。</h2><p>前者确认下载文件没有变化，后者确认应用由预期证书签名。</p></div>
        <dl className="download-hashes"><div><dt>APK 文件 SHA-256</dt><dd>制品就绪后公开</dd></div><div><dt>签名证书 SHA-256</dt><dd>制品就绪后公开</dd></div></dl>
      </section>

      <section className="download-section" aria-labelledby="support-title">
        <div className="download-section-heading"><p className="eyebrow">发布与支持</p><h2 id="support-title">每个版本，都能找到后续说明。</h2></div>
        <dl className="download-facts">{supportFields.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
      </section>

      <section className="download-section" id="install" aria-labelledby="install-title">
        <div className="download-section-heading"><p className="eyebrow">安装说明</p><h2 id="install-title">只从这里下载，只为可信来源授权。</h2></div>
        <ol className="download-steps"><li><span>01</span><div><strong>下载并核验</strong><p>入口开放后，从本页获取 APK，并对照公开的文件 SHA-256。</p></div></li><li><span>02</span><div><strong>按来源临时授权</strong><p>Android 8.0 及以上版本会要求为当前浏览器或文件管理器允许“安装未知应用”。</p></div></li><li><span>03</span><div><strong>安装后收回权限</strong><p>完成安装后，可回到系统设置关闭该来源的安装权限。</p></div></li></ol>
      </section>

      <section className="release-final download-final"><div><p className="eyebrow">当前状态</p><h2>现在还不能下载，但发布信息不会含糊。</h2></div><Link className="button" href="/">返回产品首页</Link></section>
      <footer><Link className="brand" href="/"><BrandMark /><span>SpeakUp</span></Link><p>Android 官方下载与验证信息。</p><span>© 2026 SpeakUp</span></footer>
    </main>
  );
}
