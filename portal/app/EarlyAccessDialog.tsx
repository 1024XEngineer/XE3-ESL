"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import {
  getPortalAttribution,
  getPortalSessionId,
  trackPortalEvent,
} from "./analytics";

const earlyAccessSelector = 'a[href="#early-access"]';
const scenarios = ["英文面试", "雅思口语", "海外日常", "国际职场", "其他"];

export default function EarlyAccessDialog() {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [scenario, setScenario] = useState("英文面试");
  const [status, setStatus] = useState<"idle" | "submitting" | "success" | "error">("idle");
  const [errorMessage, setErrorMessage] = useState("");

  useEffect(() => {
    trackPortalEvent("page_view");

    function openEarlyAccessDialog(event: MouseEvent) {
      if (
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey ||
        !(event.target instanceof Element)
      ) {
        return;
      }

      const trigger = event.target.closest<HTMLAnchorElement>(earlyAccessSelector);
      if (!trigger) return;

      event.preventDefault();
      const selectedScenario = trigger.dataset.scenario || "英文面试";
      setScenario(scenarios.includes(selectedScenario) ? selectedScenario : "其他");
      setStatus("idle");
      setErrorMessage("");
      trackPortalEvent("cta_click", selectedScenario);
      const dialog = dialogRef.current;
      if (dialog && !dialog.open) dialog.showModal();
    }

    document.addEventListener("click", openEarlyAccessDialog);
    return () => document.removeEventListener("click", openEarlyAccessDialog);
  }, []);

  async function submitApplication(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setStatus("submitting");
    setErrorMessage("");

    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const response = await fetch("/api/waitlist", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          sessionId: getPortalSessionId(),
          scenario: data.get("scenario"),
          urgency: data.get("urgency"),
          targetRole: data.get("targetRole"),
          challenge: data.get("challenge"),
          contact: data.get("contact"),
          website: data.get("website"),
          consent: data.get("consent") === "on",
          attribution: getPortalAttribution(),
        }),
      });
      const result = await response.json().catch(() => ({})) as {
        error?: string;
      };
      if (!response.ok) {
        setStatus("error");
        setErrorMessage(result.error || "提交失败，请稍后再试。");
        return;
      }

      setStatus("success");
      form.reset();
    } catch {
      setStatus("error");
      setErrorMessage("网络连接失败，请检查网络后重试。");
    }
  }

  return (
    <dialog
      className="early-access-modal"
      id="early-access"
      ref={dialogRef}
      aria-labelledby="early-access-title"
      aria-describedby="early-access-description"
    >
      <div className="early-access-panel">
        <form method="dialog" className="dialog-close-form">
          <button type="submit" className="dialog-close" aria-label="关闭申请表">×</button>
        </form>

        {status === "success" ? (
          <div className="application-success" role="status">
            <span aria-hidden="true">✓</span>
            <p className="eyebrow">申请已收到</p>
            <h2 id="early-access-title">谢谢你愿意<br />成为首批用户。</h2>
            <p id="early-access-description">我们会根据使用时间和场景筛选首批体验者，并通过你留下的方式联系。</p>
            <form method="dialog">
              <button className="button" type="submit" autoFocus>继续浏览</button>
            </form>
          </div>
        ) : (
          <>
            <p className="eyebrow">SpeakUp 首批体验申请</p>
            <h2 id="early-access-title">下一次必须<br />说清楚的事是什么？</h2>
            <p id="early-access-description">
              产品仍在开发中。留下真实需求，首批开放时我们会优先邀请最匹配的用户。
            </p>

            <form className="early-access-form" onSubmit={submitApplication}>
              <fieldset>
                <legend>你最想用 SpeakUp 准备什么？</legend>
                <div className="scenario-options">
                  {scenarios.map((item) => (
                    <label key={item}>
                      <input
                        type="radio"
                        name="scenario"
                        value={item}
                        checked={scenario === item}
                        onChange={() => setScenario(item)}
                      />
                      <span>{item}</span>
                    </label>
                  ))}
                </div>
              </fieldset>

              <label className="form-field">
                <span>这件事大概什么时候发生？</span>
                <select name="urgency" defaultValue="" required>
                  <option value="" disabled>请选择时间</option>
                  <option>两周内</option>
                  <option>一个月内</option>
                  <option>三个月内</option>
                  <option>先了解</option>
                </select>
              </label>

              <label className="form-field">
                <span>目标岗位或具体任务 <small>选填</small></span>
                <input name="targetRole" maxLength={160} placeholder="例如：后端开发工程师英文面试" />
              </label>

              <label className="form-field">
                <span>最容易卡住的地方 <small>选填</small></span>
                <textarea name="challenge" maxLength={500} rows={3} placeholder="例如：技术取舍说不清，遇到追问容易空白" />
              </label>

              <label className="form-field">
                <span>联系方式</span>
                <input name="contact" maxLength={160} placeholder="微信、邮箱或手机号" required />
              </label>

              <label className="honeypot" aria-hidden="true">
                网站
                <input name="website" tabIndex={-1} autoComplete="off" />
              </label>

              <label className="consent-field">
                <input type="checkbox" name="consent" required />
                <span>同意 SpeakUp 仅为首批体验邀请与我联系，不向第三方提供联系方式。</span>
              </label>

              {status === "error" && <p className="form-error" role="alert">{errorMessage}</p>}

              <button className="button" type="submit" disabled={status === "submitting"}>
                {status === "submitting" ? "正在提交…" : "申请首批体验"}
              </button>
              <p className="privacy-note">不会自动注册账号；可随时要求删除报名信息。</p>
            </form>
          </>
        )}
      </div>
    </dialog>
  );
}
