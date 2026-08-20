import {
  parseThemePreference,
  THEME_PREFERENCE_KEY,
  type ThemePreference,
  translate as t,
} from "@forgeflow/ui";

function themeIcon(theme: ThemePreference) {
  if (theme === "light") {
    return '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.65 17.65l1.42 1.42M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.65 6.35l1.42-1.42"/></svg>';
  }

  if (theme === "dark") {
    return '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M20.5 14.5A8.5 8.5 0 0 1 9.5 3.5a8.5 8.5 0 1 0 11 11Z"/></svg>';
  }

  return '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/></svg>';
}

const themeOptions: ThemePreference[] = ["system", "light", "dark"];

function loadThemePreference() {
  try {
    return parseThemePreference(localStorage.getItem(THEME_PREFERENCE_KEY));
  } catch {
    return "system" as const;
  }
}

const initialTheme = loadThemePreference();
document.documentElement.dataset.theme = initialTheme;

const root = document.querySelector<HTMLDivElement>("#root");
if (!root) throw new Error("renderer root is missing");

root.innerHTML = `
  <style>
    :root {
      color-scheme: dark;
      --canvas: #0d0f10;
      --canvas-accent: #2b2419;
      --ink: #f4f1e9;
      --ink-soft: #b4b0a7;
      --ink-dim: #77766f;
      --amber: #efb367;
      --amber-bright: #ffd08b;
      --mint: #8fe0c1;
      --accent-ink: #19130d;
      --surface: rgba(255,255,255,.045);
      --line: rgba(244,241,233,.13);
      --line-soft: rgba(244,241,233,.1);
      --field-bg: rgba(0,0,0,.2);
      --step-bg: rgba(0,0,0,.12);
      --code-bg: rgba(0,0,0,.24);
      font-family: Inter, ui-sans-serif, system-ui, sans-serif;
      color: var(--ink);
      background: var(--canvas);
    }
    :root[data-theme="light"] {
      color-scheme: light;
      --canvas: #f5f7f5;
      --canvas-accent: #eadfce;
      --ink: #18211d;
      --ink-soft: #405149;
      --ink-dim: #66746e;
      --amber: #8b5718;
      --amber-bright: #76440e;
      --mint: #246b52;
      --accent-ink: #ffffff;
      --surface: #ffffff;
      --line: rgba(24,33,29,.16);
      --line-soft: rgba(24,33,29,.1);
      --field-bg: #ffffff;
      --step-bg: rgba(24,33,29,.04);
      --code-bg: #eef2ef;
    }
    @media (prefers-color-scheme: light) {
      :root[data-theme="system"] {
        color-scheme: light;
        --canvas: #f5f7f5;
        --canvas-accent: #eadfce;
        --ink: #18211d;
        --ink-soft: #405149;
        --ink-dim: #66746e;
        --amber: #8b5718;
        --amber-bright: #76440e;
        --mint: #246b52;
        --accent-ink: #ffffff;
        --surface: #ffffff;
        --line: rgba(24,33,29,.16);
        --line-soft: rgba(24,33,29,.1);
        --field-bg: #ffffff;
        --step-bg: rgba(24,33,29,.04);
        --code-bg: #eef2ef;
      }
    }
    * { box-sizing: border-box; }
    body { margin: 0; min-width: 360px; background: radial-gradient(circle at 90% 0%, var(--canvas-accent), transparent 38%), var(--canvas); }
    .desktop-shell { min-height: 100vh; max-width: 1040px; margin: 0 auto; padding: 28px 34px; }
    .topbar { align-items: center; display: flex; justify-content: space-between; }
    .topbar-actions { align-items: center; display: flex; gap: 16px; }
    .brand { align-items: center; display: inline-flex; font: 650 15px ui-monospace, monospace; gap: 9px; letter-spacing: -.03em; }
    .mark { align-items: center; background: var(--amber); border-radius: 8px; display: inline-flex; height: 25px; justify-content: center; transform: rotate(-7deg); width: 25px; }
    .mark::after { border: 2px solid var(--accent-ink); border-bottom: 0; border-left: 0; content: ""; height: 8px; transform: rotate(45deg); width: 8px; }
    .session { align-items: center; color: var(--mint); display: inline-flex; font: 11px ui-monospace, monospace; gap: 8px; }
    .session i { background: var(--mint); border-radius: 50%; box-shadow: 0 0 0 4px color-mix(in srgb, var(--mint) 10%, transparent); height: 6px; width: 6px; }
    .theme-control { color: var(--ink-dim); display: inline-block; font: 11px ui-monospace, monospace; position: relative; }
    .theme-control-trigger { align-items: center; background: transparent; border: 1px solid transparent; border-radius: 7px; color: var(--ink-dim); cursor: pointer; display: inline-flex; height: 32px; justify-content: center; list-style: none; padding: 6px; width: 32px; }
    .theme-control-trigger::-webkit-details-marker { display: none; }
    .theme-control-trigger:hover, .theme-control[open] > .theme-control-trigger { background: var(--surface); border-color: var(--line); color: var(--ink); }
    .theme-control-trigger:focus-visible { border-color: var(--amber); outline: 2px solid var(--amber-bright); outline-offset: 1px; }
    .theme-icon { display: inline-flex; flex: 0 0 auto; height: 16px; width: 16px; }
    .theme-icon svg { display: block; height: 100%; width: 100%; }
    .theme-control-menu { background: var(--surface); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 14px 30px color-mix(in srgb, var(--shadow-base) 22%, transparent); display: grid; gap: 3px; min-width: 154px; padding: 5px; position: absolute; right: 0; top: calc(100% + 6px); z-index: 10; }
    .theme-control-menu button { align-items: center; background: transparent; border: 1px solid transparent; border-radius: 6px; color: var(--ink-soft); cursor: pointer; display: flex; font: inherit; gap: 8px; margin: 0; min-height: 32px; padding: 7px 9px; text-align: left; width: 100%; }
    .theme-control-menu button:hover, .theme-control-menu button[aria-checked="true"] { background: var(--field-bg); border-color: var(--line); color: var(--ink); transform: none; }
    .theme-control-menu button[aria-checked="true"]::after { color: var(--amber); content: "✓"; margin-left: auto; }
    .welcome { max-width: 690px; padding: 100px 0 52px; }
    .eyebrow { color: var(--amber); font: 600 11px ui-monospace, monospace; letter-spacing: .16em; margin: 0 0 18px; text-transform: uppercase; }
    h1 { font-size: clamp(40px, 7vw, 72px); letter-spacing: -.075em; line-height: .98; margin: 0; }
    h1 span { color: var(--amber); }
    .lede { color: var(--ink-soft); font-size: 17px; line-height: 1.6; margin: 22px 0 0; max-width: 570px; }
    .dashboard { display: grid; gap: 13px; grid-template-columns: 1.3fr .7fr; }
    .card { background: var(--surface); border: 1px solid var(--line); border-radius: 16px; padding: 22px; }
    .bridge-card { min-height: 218px; }
    .card-kicker { color: var(--ink-dim); font: 10px ui-monospace, monospace; letter-spacing: .12em; text-transform: uppercase; }
    .bridge-row { align-items: center; display: flex; justify-content: space-between; margin-top: 32px; }
    .bridge-title { font-size: 21px; font-weight: 650; letter-spacing: -.04em; }
    .bridge-state { color: var(--mint); font: 11px ui-monospace, monospace; }
    .bridge-state::before { background: var(--mint); border-radius: 50%; content: ""; display: inline-block; height: 6px; margin: 0 7px 1px 0; width: 6px; }
    .boundary-list { display: grid; gap: 12px; margin: 24px 0 0; }
    .boundary-list div { align-items: center; color: var(--ink-soft); display: flex; font-size: 12px; gap: 10px; }
    .boundary-list span { align-items: center; border: 1px solid color-mix(in srgb, var(--amber) 30%, transparent); border-radius: 50%; color: var(--amber); display: inline-flex; font-size: 10px; height: 20px; justify-content: center; width: 20px; }
    .check-card { display: flex; flex-direction: column; justify-content: space-between; min-height: 218px; }
    .check-icon { align-items: center; background: color-mix(in srgb, var(--mint) 8%, transparent); border: 1px solid color-mix(in srgb, var(--mint) 20%, transparent); border-radius: 11px; color: var(--mint); display: flex; font-size: 24px; height: 43px; justify-content: center; width: 43px; }
    .check-card strong { font-size: 18px; letter-spacing: -.04em; }
    .check-card small { color: var(--ink-dim); font-size: 11px; line-height: 1.5; margin-top: 8px; }
    button { background: var(--amber); border: 0; border-radius: 9px; color: var(--accent-ink); cursor: pointer; font-weight: 700; margin-top: 21px; padding: 12px 15px; transition: background .2s ease, transform .2s ease; }
    button:hover { background: var(--amber-bright); transform: translateY(-2px); }
    button:active { transform: translateY(0); }
    button:disabled { cursor: wait; opacity: .6; }
    output { color: var(--mint); display: block; font: 11px ui-monospace, monospace; margin-top: 12px; min-height: 16px; }
    .footer-note { color: var(--ink-dim); font-size: 11px; margin-top: 28px; }
    .local-tools { border-top: 1px solid var(--line-soft); display: grid; gap: 10px; margin-top: 22px; padding-top: 18px; }
    .desktop-steps { display: grid; gap: 9px; list-style: none; margin: 24px 0 0; padding: 0; }
    .desktop-step { background: var(--step-bg); border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
    .desktop-step[open] { border-color: color-mix(in srgb, var(--amber) 35%, transparent); }
    .desktop-step-heading { align-items: center; cursor: pointer; display: grid; gap: 11px; grid-template-columns: 28px minmax(0,1fr) auto; list-style: none; padding: 14px; }
    .desktop-step-heading::-webkit-details-marker { display: none; }
    .desktop-step-heading::after { color: var(--ink-dim); content: "＋"; font-size: 16px; }
    .desktop-step[open] > .desktop-step-heading::after { content: "−"; }
    .desktop-step-heading > span:nth-child(2) { display: grid; gap: 4px; }
    .desktop-step-heading strong { font-size: 13px; }
    .desktop-step-heading small { color: var(--ink-dim); font-size: 11px; line-height: 1.4; }
    .step-number { align-items: center; background: color-mix(in srgb, var(--amber) 10%, transparent); border: 1px solid color-mix(in srgb, var(--amber) 35%, transparent); border-radius: 50%; color: var(--amber); display: inline-flex; font: 700 11px ui-monospace, monospace; height: 27px; justify-content: center; width: 27px; }
    .step-status { color: var(--ink-dim); font: 10px ui-monospace, monospace; margin: 0; text-align: right; }
    .step-status.is-ready { color: var(--mint); }
    .desktop-step > .local-tools, .desktop-step > .agent-tools { border-top: 1px solid var(--line-soft); margin: 0; padding: 15px; }
    .desktop-step .local-tools { border-top: 0; }
    .desktop-step .agent-tools { border-top: 1px solid var(--line-soft); }
    .desktop-step .local-actions { margin-top: 2px; }
    .local-tools label { color: var(--ink-soft); display: grid; font-size: 11px; gap: 6px; }
    .local-tools input { background: var(--field-bg); border: 1px solid var(--line); border-radius: 7px; color: var(--ink); font: 12px ui-monospace, monospace; padding: 10px; }
    .repo-picker { display: flex !important; gap: 8px; }
    .repo-picker input { flex: 1; min-width: 0; }
    .repo-picker button { margin: 0; padding: 9px 11px; }
    .local-actions { display: flex; flex-wrap: wrap; gap: 8px; }
    .local-actions button { margin-top: 0; }
    .commit-tools { display: grid; gap: 10px; }
    .diff-output { background: var(--code-bg); border: 1px solid var(--line); border-radius: 8px; color: var(--ink-soft); font: 11px/1.5 ui-monospace, monospace; max-height: 260px; margin: 0; overflow: auto; padding: 12px; white-space: pre-wrap; }
    .agent-tools { border-top: 1px solid var(--line-soft); display: grid; gap: 10px; margin-top: 18px; padding-top: 18px; }
    .agent-tools label { color: var(--ink-soft); display: grid; font-size: 11px; gap: 6px; }
    .agent-tools select, .agent-tools textarea { background: var(--field-bg); border: 1px solid var(--line); border-radius: 7px; color: var(--ink); font: 12px ui-monospace, monospace; padding: 10px; }
    .agent-tools textarea { min-height: 90px; resize: vertical; }
    .approval { align-items: center !important; display: flex !important; flex-direction: row; gap: 8px !important; }
    .approval input { accent-color: var(--amber); }
    .server-sync { border-top: 1px solid var(--line-soft); margin-top: 8px; padding-top: 14px; }
    .server-sync summary { color: var(--amber); cursor: pointer; font-size: 12px; font-weight: 650; }
    .server-sync p { color: var(--ink-dim); font-size: 11px; line-height: 1.5; margin: 9px 0; }
    .auth-state { color: var(--mint); font: 11px ui-monospace, monospace; min-height: 16px; }
    :focus-visible { outline: 2px solid var(--amber-bright); outline-offset: 3px; }
    @media (max-width: 700px) { .desktop-shell { padding: 22px; }.topbar { align-items: flex-start; gap: 12px; }.topbar-actions { align-items: flex-end; flex-direction: column; gap: 8px; }.welcome { padding: 72px 0 38px; }.dashboard { grid-template-columns: 1fr; } }
    @media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation-duration: .01ms !important; transition-duration: .01ms !important; } }
  </style>
  <div class="desktop-shell">
    <header class="topbar"><div class="brand"><span class="mark" aria-hidden="true"></span><span>forgeflow</span></div><div class="topbar-actions"><details class="theme-control" id="theme-control"><summary class="theme-control-trigger" aria-label="${t("nav.theme")}" aria-haspopup="menu" title="${t("nav.theme")}: ${t(`theme.${initialTheme}`)}"><span class="theme-icon">${themeIcon(initialTheme)}</span></summary><div class="theme-control-menu" role="menu" aria-label="${t("nav.theme")}">${themeOptions.map((theme) => `<button type="button" role="menuitemradio" aria-checked="${theme === initialTheme}" data-theme-option="${theme}"><span class="theme-icon">${themeIcon(theme)}</span><span>${t(`theme.${theme}`)}</span></button>`).join("")}</div></details><span class="session"><i aria-hidden="true"></i> ${t("desktop.local-session")}</span></div></header>
    <section class="welcome" aria-labelledby="desktop-heading"><p class="eyebrow">${t("desktop.workspace")}</p><h1 id="desktop-heading">${t("desktop.heading-before")}<span>${t("desktop.heading-highlight")}</span></h1><p class="lede">${t("desktop.lede")}</p></section>
    <section class="dashboard" aria-label="${t("desktop.overview")}">
    <article class="card bridge-card"><span class="card-kicker">${t("desktop.local-bridge")}</span><div class="bridge-row"><strong class="bridge-title">${t("desktop.ready")}</strong><span class="bridge-state">${t("desktop.protected")}</span></div><div class="boundary-list"><div><span aria-hidden="true">✓</span> ${t("desktop.boundary-ipc")}</div><div><span aria-hidden="true">✓</span> ${t("desktop.boundary-worktree")}</div><div><span aria-hidden="true">✓</span> ${t("desktop.boundary-human")}</div></div><button id="health" type="button">${t("desktop.health")} <span aria-hidden="true">↗</span></button><output id="result" aria-live="polite"></output><ol class="desktop-steps" aria-label="${t("desktop.execution-steps")}"><li><details class="desktop-step" id="step-repository" open><summary class="desktop-step-heading"><span class="step-number">1</span><span><strong>${t("desktop.step-repository")}</strong><small>${t("desktop.step-repository-description")}</small></span><output id="repo-step-status" class="step-status" aria-live="polite">${t("desktop.step-needed")}</output></summary><div class="local-tools"><label class="repo-picker" for="repo-root">${t("desktop.repo-path")}<input id="repo-root" placeholder="/Users/you/code/project" autocomplete="off" /><button id="choose-repository" type="button">${t("desktop.choose-folder")}</button></label><div class="local-actions"><button id="repo-status" type="button">${t("desktop.repo-status")}</button></div></div></details></li><li><details class="desktop-step" id="step-worktree"><summary class="desktop-step-heading"><span class="step-number">2</span><span><strong>${t("desktop.step-worktree")}</strong><small>${t("desktop.step-worktree-description")}</small></span><output id="worktree-step-status" class="step-status" aria-live="polite">${t("desktop.step-locked")}</output></summary><div class="local-tools"><label for="worktree-name">${t("desktop.worktree-name")}<input id="worktree-name" value="agent-run" maxlength="80" autocomplete="off" /></label><label for="branch-name">${t("desktop.branch-name")}<input id="branch-name" value="forgeflow/agent-run" maxlength="128" autocomplete="off" /></label><label for="base-ref">${t("desktop.base-ref")}<input id="base-ref" value="HEAD" maxlength="256" autocomplete="off" /></label><div class="local-actions"><button id="create-worktree" type="button">${t("desktop.create-worktree")}</button></div></div></details></li><li><details class="desktop-step" id="step-review"><summary class="desktop-step-heading"><span class="step-number">3</span><span><strong>${t("desktop.step-review")}</strong><small>${t("desktop.step-review-description")}</small></span><output id="review-step-status" class="step-status" aria-live="polite">${t("desktop.step-locked")}</output></summary><div class="local-tools"><div class="local-actions"><button id="inspect-diff" type="button" disabled>${t("desktop.inspect-diff")}</button><button id="remove-worktree" type="button" disabled>${t("desktop.remove-worktree")}</button></div><div class="commit-tools"><label for="commit-message">${t("desktop.commit-message")}<input id="commit-message" value="Implement approved change" maxlength="200" autocomplete="off" /></label><div class="local-actions"><button id="commit-worktree" type="button" disabled>${t("desktop.commit-changes")}</button><button id="push-worktree" type="button" disabled>${t("desktop.push-branch")}</button></div></div><output id="local-result" aria-live="polite"></output><pre id="diff-output" class="diff-output" hidden></pre></div></details></li><li><details class="desktop-step" id="step-agent"><summary class="desktop-step-heading"><span class="step-number">4</span><span><strong>${t("desktop.step-agent")}</strong><small>${t("desktop.step-agent-description")}</small></span><output id="agent-step-status" class="step-status" aria-live="polite">${t("desktop.step-locked")}</output></summary><div class="agent-tools"><label for="agent-provider">${t("desktop.provider")}<select id="agent-provider"><option value="codex">Codex</option><option value="claude">Claude</option></select></label><label for="agent-prompt">${t("desktop.task-prompt")}<textarea id="agent-prompt" maxlength="131072" placeholder="${t("desktop.task-prompt-placeholder")}"></textarea></label><label class="approval" for="agent-approved"><input id="agent-approved" type="checkbox" /> ${t("desktop.approval")}</label><details class="server-sync"><summary>${t("desktop.sync-summary")}</summary><p>${t("desktop.sync-description")}</p><label for="forgeflow-api-url">${t("desktop.api-url")}<input id="forgeflow-api-url" placeholder="https://forgeflow.example.com" autocomplete="url" /></label><div class="local-actions"><button id="desktop-sign-in" type="button">${t("desktop.sign-in")}</button><button id="desktop-sign-out" type="button">${t("desktop.sign-out")}</button></div><output id="desktop-auth-result" class="auth-state" aria-live="polite"></output><label for="forgeflow-pat">${t("desktop.pat")}<input id="forgeflow-pat" type="password" maxlength="512" autocomplete="off" /></label><label for="forgeflow-project-id">${t("desktop.project-id")}<input id="forgeflow-project-id" maxlength="128" autocomplete="off" /></label><label for="forgeflow-run-id">${t("desktop.approved-run-id")}<input id="forgeflow-run-id" maxlength="128" autocomplete="off" /></label></details><div class="local-actions"><button id="run-agent" type="button">${t("desktop.run-agent")}</button><button id="cancel-agent" type="button" disabled>${t("desktop.cancel-agent")}</button></div><output id="agent-result" aria-live="polite"></output></div></details></li></ol></article>
      <article class="card check-card"><div class="check-icon" aria-hidden="true">✦</div><div><strong>${t("desktop.evidence-title")}</strong><small>${t("desktop.evidence-description")}</small></div></article>
    </section>
    <p class="footer-note">${t("desktop.footer")}</p>
  </div>
`;

const themeControl = document.querySelector<HTMLDetailsElement>("#theme-control");
const themeTrigger = themeControl?.querySelector<HTMLElement>(".theme-control-trigger");
const themeTriggerIcon = themeControl?.querySelector<HTMLElement>(".theme-control-trigger .theme-icon");
const themeOptionButtons = Array.from(
  themeControl?.querySelectorAll<HTMLButtonElement>("[data-theme-option]") ?? [],
);
function applyTheme(theme: ThemePreference) {
  document.documentElement.dataset.theme = theme;
  if (themeTriggerIcon) themeTriggerIcon.innerHTML = themeIcon(theme);
  themeTrigger?.setAttribute("title", `${t("nav.theme")}: ${t(`theme.${theme}`)}`);
  themeOptionButtons.forEach((option) => {
    option.setAttribute("aria-checked", String(option.dataset.themeOption === theme));
  });
  try {
    localStorage.setItem(THEME_PREFERENCE_KEY, theme);
  } catch {
    // The selected theme still applies until this renderer closes.
  }
  if (themeControl) themeControl.open = false;
}
themeOptionButtons.forEach((option) => {
  option.addEventListener("click", () => {
    applyTheme(parseThemePreference(option.dataset.themeOption));
  });
});

const button = document.querySelector<HTMLButtonElement>("#health");
const result = document.querySelector<HTMLOutputElement>("#result");
button?.addEventListener("click", async () => {
  if (!result || !button) return;
  button.disabled = true;
  button.textContent = t("desktop.health-checking");
  result.textContent = "";
  try {
    const health = await window.forgeflow.health();
    result.textContent = health.ok
      ? t("desktop.ipc-ready")
      : t("desktop.ipc-unavailable");
  } catch {
    result.textContent = t("desktop.ipc-failed");
  } finally {
    button.disabled = false;
    button.innerHTML = `${t("desktop.health")} <span aria-hidden="true">↗</span>`;
  }
});

const sourceRoot = document.querySelector<HTMLInputElement>("#repo-root");
const chooseRepositoryButton = document.querySelector<HTMLButtonElement>("#choose-repository");
const worktreeName = document.querySelector<HTMLInputElement>("#worktree-name");
const branchName = document.querySelector<HTMLInputElement>("#branch-name");
const baseRef = document.querySelector<HTMLInputElement>("#base-ref");
const statusButton = document.querySelector<HTMLButtonElement>("#repo-status");
const createButton =
  document.querySelector<HTMLButtonElement>("#create-worktree");
const inspectDiffButton =
  document.querySelector<HTMLButtonElement>("#inspect-diff");
const removeWorktreeButton =
  document.querySelector<HTMLButtonElement>("#remove-worktree");
const commitMessage = document.querySelector<HTMLInputElement>("#commit-message");
const commitButton = document.querySelector<HTMLButtonElement>("#commit-worktree");
const pushButton = document.querySelector<HTMLButtonElement>("#push-worktree");
const localResult = document.querySelector<HTMLOutputElement>("#local-result");
const diffOutput = document.querySelector<HTMLElement>("#diff-output");
const agentProvider = document.querySelector<HTMLSelectElement>("#agent-provider");
const agentPrompt = document.querySelector<HTMLTextAreaElement>("#agent-prompt");
const agentApproved = document.querySelector<HTMLInputElement>("#agent-approved");
const forgeflowAPIURL = document.querySelector<HTMLInputElement>("#forgeflow-api-url");
const forgeflowPAT = document.querySelector<HTMLInputElement>("#forgeflow-pat");
const forgeflowProjectID = document.querySelector<HTMLInputElement>("#forgeflow-project-id");
const forgeflowRunID = document.querySelector<HTMLInputElement>("#forgeflow-run-id");
const desktopSignInButton = document.querySelector<HTMLButtonElement>("#desktop-sign-in");
const desktopSignOutButton = document.querySelector<HTMLButtonElement>("#desktop-sign-out");
const desktopAuthResult = document.querySelector<HTMLOutputElement>("#desktop-auth-result");
const runButton = document.querySelector<HTMLButtonElement>("#run-agent");
const cancelButton = document.querySelector<HTMLButtonElement>("#cancel-agent");
const agentResult = document.querySelector<HTMLOutputElement>("#agent-result");
const repositoryStep = document.querySelector<HTMLDetailsElement>("#step-repository");
const worktreeStep = document.querySelector<HTMLDetailsElement>("#step-worktree");
const reviewStep = document.querySelector<HTMLDetailsElement>("#step-review");
const agentStep = document.querySelector<HTMLDetailsElement>("#step-agent");
const repositoryStepStatus = document.querySelector<HTMLOutputElement>("#repo-step-status");
const worktreeStepStatus = document.querySelector<HTMLOutputElement>("#worktree-step-status");
const reviewStepStatus = document.querySelector<HTMLOutputElement>("#review-step-status");
const agentStepStatus = document.querySelector<HTMLOutputElement>("#agent-step-status");
let managedWorktree = "";
let activeAgentRunID = "";

function setStepStatus(element: HTMLOutputElement | null, message: string, ready: boolean) {
  if (!element) return;
  element.textContent = message;
  element.classList.toggle("is-ready", ready);
}

function refreshStepStates() {
  const repositoryReady = Boolean(sourceRoot?.value.trim());
  const worktreeReady = Boolean(managedWorktree);
  if (!repositoryReady && repositoryStep) repositoryStep.open = true;
  setStepStatus(repositoryStepStatus, repositoryReady ? t("desktop.step-ready") : t("desktop.step-needed"), repositoryReady);
  setStepStatus(worktreeStepStatus, worktreeReady ? t("desktop.step-ready") : t("desktop.step-locked"), worktreeReady);
  setStepStatus(reviewStepStatus, worktreeReady ? t("desktop.step-ready") : t("desktop.step-locked"), worktreeReady);
  setStepStatus(agentStepStatus, activeAgentRunID ? t("desktop.step-running") : worktreeReady ? t("desktop.step-ready") : t("desktop.step-locked"), Boolean(activeAgentRunID || worktreeReady));
}

refreshStepStates();
sourceRoot?.addEventListener("input", refreshStepStates);

void window.forgeflow.agentRecoveries().then((recoveries) => {
  const recoverable = recoveries.find((item) =>
    ["RUNNING", "RECOVERY_REQUIRED"].includes(item.phase),
  );
  if (recoverable && agentResult) {
    agentResult.textContent = t("desktop.recovery", { id: recoverable.runID ?? t("desktop.unknown"), status: recoverable.serverStatus ?? recoverable.phase });
  }
}).catch(() => {
  // Recovery metadata is best effort and must not prevent local execution.
});

async function refreshDesktopAuth() {
  if (!desktopAuthResult) return;
  try {
    const auth = await window.forgeflow.authStatus();
    desktopAuthResult.textContent = auth.signedIn
      ? t("desktop.auth-ready", { url: auth.apiBaseURL })
      : t("desktop.no-session");
    if (auth.signedIn && forgeflowAPIURL && !forgeflowAPIURL.value) forgeflowAPIURL.value = auth.apiBaseURL;
  } catch {
    desktopAuthResult.textContent = t("desktop.auth-unavailable");
  }
}

void refreshDesktopAuth();

desktopSignInButton?.addEventListener("click", async () => {
  if (!forgeflowAPIURL?.value.trim() || !desktopAuthResult || !desktopSignInButton) return;
  desktopSignInButton.disabled = true;
  desktopAuthResult.textContent = t("desktop.opening-sign-in");
  try {
    const auth = await window.forgeflow.signIn(forgeflowAPIURL.value.trim());
    desktopAuthResult.textContent = auth.signedIn ? t("desktop.auth-ready", { url: auth.apiBaseURL }) : t("desktop.signin-incomplete");
  } catch (error) {
    desktopAuthResult.textContent = error instanceof Error ? error.message : t("desktop.signin-failed");
  } finally {
    desktopSignInButton.disabled = false;
  }
});

desktopSignOutButton?.addEventListener("click", async () => {
  if (!desktopAuthResult || !desktopSignOutButton) return;
  desktopSignOutButton.disabled = true;
  try {
    await window.forgeflow.signOut();
    desktopAuthResult.textContent = t("desktop.session-removed");
  } finally {
    desktopSignOutButton.disabled = false;
  }
});

chooseRepositoryButton?.addEventListener("click", async () => {
  if (!sourceRoot || !chooseRepositoryButton) return;
  chooseRepositoryButton.disabled = true;
  try {
    const selected = await window.forgeflow.chooseRepository();
    if (selected) {
      sourceRoot.value = selected;
      if (worktreeStep) worktreeStep.open = true;
      refreshStepStates();
    }
  } finally {
    chooseRepositoryButton.disabled = false;
  }
});

statusButton?.addEventListener("click", async () => {
  if (!sourceRoot?.value.trim() || !localResult || !statusButton) return;
  statusButton.disabled = true;
  localResult.textContent = t("desktop.checking");
  try {
    const status = await window.forgeflow.repositoryStatus(
      sourceRoot.value.trim(),
    );
    localResult.textContent = `${status.branch || "detached"} · ${status.clean ? t("desktop.clean") : t("desktop.local-changes")}`;
    if (worktreeStep) worktreeStep.open = true;
    refreshStepStates();
  } catch (error) {
    localResult.textContent =
      error instanceof Error ? error.message : t("desktop.repo-failed");
  } finally {
    statusButton.disabled = false;
  }
});

createButton?.addEventListener("click", async () => {
  if (
    !sourceRoot?.value.trim() ||
    !worktreeName?.value.trim() ||
    !localResult ||
    !createButton
  )
    return;
  createButton.disabled = true;
  localResult.textContent = t("desktop.creating-worktree");
  try {
    const status = await window.forgeflow.createWorktree({
      sourceRoot: sourceRoot.value.trim(),
      name: worktreeName.value.trim(),
      baseRef: baseRef?.value.trim() || "HEAD",
      branchName: branchName?.value.trim() || undefined,
    });
    managedWorktree = status.root;
    if (inspectDiffButton) inspectDiffButton.disabled = false;
    if (removeWorktreeButton) removeWorktreeButton.disabled = false;
    if (commitButton) commitButton.disabled = false;
    if (pushButton) pushButton.disabled = false;
    if (reviewStep) reviewStep.open = true;
    refreshStepStates();
    localResult.textContent = t("desktop.worktree-created", { root: status.root, branch: status.branch || "detached" });
  } catch (error) {
    localResult.textContent =
      error instanceof Error ? error.message : t("desktop.worktree-failed");
  } finally {
    createButton.disabled = false;
  }
});

commitButton?.addEventListener("click", async () => {
  if (!managedWorktree || !commitMessage?.value.trim() || !localResult || !commitButton) return;
  commitButton.disabled = true;
  localResult.textContent = t("desktop.committing");
  try {
    const result = await window.forgeflow.commitWorktree({
      candidate: managedWorktree,
      message: commitMessage.value,
    });
    localResult.textContent = t("desktop.committed", { sha: result.commitSHA.slice(0, 12), branch: result.branch });
  } catch (error) {
    localResult.textContent = error instanceof Error ? error.message : t("desktop.commit-failed");
  } finally {
    commitButton.disabled = false;
  }
});

pushButton?.addEventListener("click", async () => {
  if (!managedWorktree || !localResult || !pushButton) return;
  if (!window.confirm(t("desktop.push-confirm"))) return;
  pushButton.disabled = true;
  localResult.textContent = t("desktop.pushing");
  try {
    const result = await window.forgeflow.pushWorktree(managedWorktree);
    localResult.textContent = t("desktop.pushed", { branch: result.branch });
  } catch (error) {
    localResult.textContent = error instanceof Error ? error.message : t("desktop.push-failed");
  } finally {
    pushButton.disabled = false;
  }
});

inspectDiffButton?.addEventListener("click", async () => {
  if (!managedWorktree || !localResult || !diffOutput || !inspectDiffButton) return;
  inspectDiffButton.disabled = true;
  localResult.textContent = t("desktop.reading-diff");
  try {
    const diff = await window.forgeflow.repositoryDiff(managedWorktree);
    diffOutput.hidden = false;
    diffOutput.textContent = diff.files.length
      ? `${t("desktop.diff-files", { count: diff.files.length, files: diff.files.join(", ") })}\n\n${diff.patch || t("desktop.no-text-diff")}`
      : t("desktop.no-changes");
    if (agentStep) agentStep.open = true;
    localResult.textContent = t("desktop.diff-ready");
  } catch (error) {
    localResult.textContent = error instanceof Error ? error.message : t("desktop.diff-failed");
  } finally {
    inspectDiffButton.disabled = false;
  }
});

removeWorktreeButton?.addEventListener("click", async () => {
  if (!managedWorktree || !localResult || !removeWorktreeButton) return;
  removeWorktreeButton.disabled = true;
  try {
    await window.forgeflow.removeWorktree(managedWorktree);
    managedWorktree = "";
    if (worktreeStep) worktreeStep.open = true;
    if (reviewStep) reviewStep.open = false;
    if (agentStep) agentStep.open = false;
    if (inspectDiffButton) inspectDiffButton.disabled = true;
    if (commitButton) commitButton.disabled = true;
    if (pushButton) pushButton.disabled = true;
    if (diffOutput) {
      diffOutput.hidden = true;
      diffOutput.textContent = "";
    }
    refreshStepStates();
    localResult.textContent = t("desktop.worktree-cleaned");
  } catch (error) {
    removeWorktreeButton.disabled = false;
    localResult.textContent = error instanceof Error ? error.message : t("desktop.cleanup-failed");
  }
});

runButton?.addEventListener("click", async () => {
  if (!runButton || !agentResult || !agentProvider || !agentPrompt || !agentApproved) return;
  if (!managedWorktree) {
    agentResult.textContent = t("desktop.need-worktree");
    return;
  }
  if (!agentPrompt.value.trim()) {
    agentResult.textContent = t("desktop.prompt-required");
    return;
  }
  const syncFields = [forgeflowAPIURL, forgeflowPAT, forgeflowProjectID, forgeflowRunID];
  const syncRequested = syncFields.some((field) => Boolean(field?.value.trim()));
  const requiredSyncFields = [forgeflowAPIURL, forgeflowProjectID, forgeflowRunID];
  if (syncRequested && requiredSyncFields.some((field) => !field?.value.trim())) {
    agentResult.textContent = t("desktop.sync-fields");
    return;
  }
  runButton.disabled = true;
  if (cancelButton) cancelButton.disabled = false;
  activeAgentRunID = typeof crypto.randomUUID === "function" ? crypto.randomUUID() : `run-${Date.now()}`;
  if (agentStep) agentStep.open = true;
  refreshStepStates();
  agentResult.textContent = t("desktop.local-running");
  try {
    const server = syncRequested
      ? {
          apiBaseURL: forgeflowAPIURL?.value.trim() ?? "",
          token: forgeflowPAT?.value ?? "",
          projectID: forgeflowProjectID?.value.trim() ?? "",
          runID: forgeflowRunID?.value.trim() ?? "",
        }
      : undefined;
    const result = await window.forgeflow.runAgent({
      runID: activeAgentRunID,
      provider: agentProvider.value as "codex" | "claude",
      worktree: managedWorktree,
      prompt: agentPrompt.value,
      approved: agentApproved.checked,
      baseCommit: baseRef?.value.trim() || "HEAD",
      server,
    });
    const outcome = result.cancelled ? t("desktop.cancelled") : result.timedOut ? t("desktop.timed-out") : t("desktop.exited", { code: result.code ?? result.signal ?? t("desktop.unknown") });
    const syncStatus = server ? result.serverSyncError ? ` · ${t("desktop.server-sync-failed", { error: result.serverSyncError })}` : ` · ${t("desktop.server-ready")}` : "";
    agentResult.textContent = `${outcome}${syncStatus} · ${result.output || t("desktop.no-output")}`;
  } catch (error) {
    agentResult.textContent = error instanceof Error ? error.message : t("desktop.agent-failed");
  } finally {
    runButton.disabled = false;
    if (cancelButton) cancelButton.disabled = true;
    activeAgentRunID = "";
    refreshStepStates();
  }
});

cancelButton?.addEventListener("click", async () => {
  if (!activeAgentRunID || !agentResult || !cancelButton) return;
  cancelButton.disabled = true;
  agentResult.textContent = t("desktop.cancelling");
  try {
    const result = await window.forgeflow.cancelAgent(activeAgentRunID);
    if (!result.cancelled) agentResult.textContent = t("desktop.already-stopped");
  } catch (error) {
    agentResult.textContent = error instanceof Error ? error.message : t("desktop.cancel-failed");
  }
});
