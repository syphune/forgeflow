import { WorkItemWorkspace } from "./work-item-workspace";
import { AuthStatus } from "./auth-status";
import { redirect } from "next/navigation";
import { translate as t } from "@forgeflow/ui";
import { ThemeControl } from "@/features/app/theme-control";

export const dynamic = "force-dynamic";

const apiBase = (process.env.NEXT_PUBLIC_FORGEFLOW_API_URL ?? "").replace(/\/+$/, "");
const signInURL = `${apiBase}/api/v1/auth/github/start`;
const meURL = `${apiBase}/api/v1/me`;

const checks = [
  { index: "01", label: t("landing.check-process-label"), value: t("landing.check-process-value"), icon: "↗" },
  { index: "02", label: t("landing.check-spec-label"), value: t("landing.check-spec-value"), icon: "✦" },
  { index: "03", label: t("landing.check-evidence-label"), value: t("landing.check-evidence-value"), icon: "◌" },
];

export default function HomePage() {
  if (process.env.FORGEFLOW_UI_MODE !== "legacy") redirect("/app");
  return (
    <main className="app-shell">
      <div className="ambient ambient-one" aria-hidden="true" />
      <div className="ambient ambient-two" aria-hidden="true" />

      <header className="topbar">
          <a className="brand" href="#top" aria-label={t("landing.home")}>
          <span className="brand-mark" aria-hidden="true"><span /></span>
          <span>forgeflow</span>
        </a>
        <nav className="topnav" aria-label={t("landing.primary-navigation")}>
          <a href="#principles">{t("landing.principles")}</a>
          <a href="#workspace">{t("landing.workspace")}</a>
          <ThemeControl className="theme-control-topnav" />
          <AuthStatus signInURL={signInURL} meURL={meURL} />
        </nav>
      </header>

      <div id="top" className="page-content">
        <section className="hero" aria-labelledby="hero-heading">
          <div className="hero-copy">
            <p className="eyebrow"><span className="live-dot" aria-hidden="true" /> {t("landing.hero-eyebrow")}</p>
            <h1 id="hero-heading">{t("landing.hero-title-before")}<em>{t("landing.hero-title-emphasis")}</em></h1>
            <p className="lede">{t("landing.hero-description")}</p>
            <div className="hero-actions">
              <a className="button button-primary" href="#workspace">{t("landing.open-workspace")} <span aria-hidden="true">↓</span></a>
              <a className="text-link" href={signInURL}>{t("app.continue-github")} <span aria-hidden="true">↗</span></a>
            </div>
            <p className="microcopy"><span className="secure-mark" aria-hidden="true">✓</span> {t("landing.microcopy")}</p>
          </div>

          <div className="hero-visual" role="img" aria-label={t("landing.preview")}>
            <div className="signal-card signal-card-main">
              <div className="signal-card-top"><span className="signal-label">{t("landing.signal-current")}</span><span className="signal-live">{t("landing.signal-live")}</span></div>
              <div className="signal-title">{t("landing.signal-title-line1")}<br /><span>{t("landing.signal-title-line2")}</span></div>
              <div className="signal-meta"><span className="signal-key">FF-124</span><span className="signal-status"><i /> {t("landing.signal-status")}</span></div>
              <div className="signal-progress"><span /><span /><span /><span className="is-muted" /></div>
              <div className="signal-footer"><span>{t("landing.evidence-coverage")}</span><strong>82%</strong></div>
            </div>
            <div className="signal-card signal-card-note"><span className="note-icon" aria-hidden="true">✦</span><div><strong>{t("landing.human-verified")}</strong><small>{t("landing.expected-behavior")}</small></div></div>
            <div className="signal-orbit orbit-one" aria-hidden="true" />
            <div className="signal-orbit orbit-two" aria-hidden="true" />
          </div>
        </section>

        <section id="principles" className="principles" aria-labelledby="principles-heading">
          <div className="section-intro"><p className="eyebrow">{t("landing.foundation")}</p><h2 id="principles-heading">{t("landing.foundation-title")}</h2><p>{t("landing.foundation-description")}</p></div>
          <div className="check-grid">
            {checks.map((check) => (
              <article className="check-card" key={check.label}>
                <div className="check-card-top"><span className="check-index">{check.index}</span><span className="check-icon" aria-hidden="true">{check.icon}</span></div>
                <p>{check.label}</p>
                <strong>{check.value}</strong>
              </article>
            ))}
          </div>
        </section>

        <section id="workspace" className="workspace-section" aria-labelledby="workspace-heading">
          <div className="section-heading"><div><p className="eyebrow">{t("landing.focus")}</p><h2 id="workspace-heading">{t("landing.next-step")}</h2></div><span className="section-badge"><span className="live-dot" aria-hidden="true" /> {t("landing.calm-default")}</span></div>
          <WorkItemWorkspace />
        </section>

        <footer className="footer"><a className="brand footer-brand" href="#top"><span className="brand-mark" aria-hidden="true"><span /></span><span>forgeflow</span></a><span>{t("landing.footer-tagline")}</span><a href={signInURL}>{t("landing.start")} <span aria-hidden="true">↗</span></a></footer>
      </div>
    </main>
  );
}
