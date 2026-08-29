#!/usr/bin/env node

import { execFile, execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { performance } from "node:perf_hooks";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const apiURL = (process.env.FORGEFLOW_PERF_API_URL ?? "http://127.0.0.1:18080").replace(/\/+$/, "");
const webURL = (process.env.FORGEFLOW_PERF_WEB_URL ?? "http://127.0.0.1:13000").replace(/\/+$/, "");
const context = {
  organizationID: process.env.FORGEFLOW_PERF_ORGANIZATION_ID ?? "00000000-0000-0000-0000-000000000001",
  actorID: process.env.FORGEFLOW_PERF_ACTOR_ID ?? "00000000-0000-0000-0000-000000000004",
  workspaceID: process.env.FORGEFLOW_PERF_WORKSPACE_ID ?? "00000000-0000-0000-0000-000000000002",
  projectID: process.env.FORGEFLOW_PERF_PROJECT_ID ?? "00000000-0000-0000-0000-000000000003",
  workItemID: process.env.FORGEFLOW_PERF_WORK_ITEM_ID ?? "475e73eb-5309-4ec8-b5db-01553634d8bf",
};
const iterations = Number(process.env.FORGEFLOW_PERF_ITERATIONS ?? "12");
const concurrency = Number(process.env.FORGEFLOW_PERF_CONCURRENCY ?? "8");
const timeoutMs = Number(process.env.FORGEFLOW_PERF_TIMEOUT_MS ?? "15000");
const uniqueLoadRateKeys = process.env.FORGEFLOW_PERF_UNIQUE_RATE_KEYS === "true";
const uniqueBrowserRateKeys = process.env.FORGEFLOW_PERF_UNIQUE_BROWSER_RATE_KEYS === "true";
const thresholds = {
  apiP95Ms: Number(process.env.FORGEFLOW_PERF_API_P95_MS ?? "750"),
  apiP99Ms: Number(process.env.FORGEFLOW_PERF_API_P99_MS ?? "1500"),
  webP95Ms: Number(process.env.FORGEFLOW_PERF_WEB_P95_MS ?? "5000"),
  webP99Ms: Number(process.env.FORGEFLOW_PERF_WEB_P99_MS ?? "8000"),
  heapGrowthMB: Number(process.env.FORGEFLOW_PERF_HEAP_GROWTH_MB ?? "64"),
};

const failures = [];

function parseOperations() {
  const content = fs.readFileSync(path.join(root, "contracts/openapi/openapi.yaml"), "utf8");
  const operations = [];
  let currentPath = "";
  let currentMethod = "";
  for (const line of content.split("\n")) {
    const pathMatch = line.match(/^  (\/[^:]+):\s*$/);
    if (pathMatch) {
      currentPath = pathMatch[1];
      currentMethod = "";
      continue;
    }
    const methodMatch = line.match(/^    (get|post|put|patch|delete):\s*$/);
    if (methodMatch) {
      currentMethod = methodMatch[1].toUpperCase();
      continue;
    }
    const operationMatch = line.match(/^      operationId:\s*(\S+)\s*$/);
    if (operationMatch && currentPath && currentMethod) {
      operations.push({ id: operationMatch[1], method: currentMethod, path: currentPath });
      currentMethod = "";
    }
  }
  return operations;
}

function operationURL(operation) {
  const replacements = {
    id: "missing-id",
    user_id: "missing-user",
    repository_id: "missing-repository",
    field_id: "missing-field",
    rule_id: "missing-rule",
    comment_id: "missing-comment",
    attachment_id: "missing-attachment",
    proposal_id: "missing-proposal",
    snapshot_id: "missing-snapshot",
    document_id: "missing-document",
  };
  const pathname = operation.path.replace(/\{([^}]+)\}/g, (_, key) => replacements[key] ?? "missing-id");
  const url = new URL(`/api/v1${pathname}`, `${apiURL}/`);
  const query = new URLSearchParams();
  if (operation.id === "listGitHubRepositories" || operation.id === "getSprintContext") query.set("project_id", context.projectID);
  if (["repositoryFile", "getRepositoryFile"].includes(operation.id)) query.set("path", "README.md");
  if (["searchRepository", "searchRepositorySnapshot"].includes(operation.id)) {
    query.set("q", "astro");
    query.set("limit", "10");
  }
  if (operation.id === "listRelatedRepositoryFiles") {
    query.set("path", "README.md");
    query.set("limit", "10");
  }
  if (["listRepositorySnapshots", "listKnowledge", "listKnowledgeRevisions", "listNotifications", "listAuditRecords"].includes(operation.id)) query.set("limit", "10");
  url.search = query.toString();
  return url;
}

function requestHeaders() {
  const headers = {
    accept: "application/json",
    "x-organization-id": context.organizationID,
    "x-actor-id": context.actorID,
    "x-project-id": context.projectID,
  };
  if (process.env.FORGEFLOW_PERF_AUTHORIZATION) headers.authorization = process.env.FORGEFLOW_PERF_AUTHORIZATION;
  return headers;
}

function configuredViewports() {
  return (process.env.FORGEFLOW_PERF_WEB_VIEWPORTS ?? "desktop:1280x900")
    .split(",")
    .map((value, index) => {
      const match = value.trim().match(/^(?:([\w-]+):)?(\d+)x(\d+)$/);
      if (!match) throw new Error(`invalid viewport: ${value}`);
      return { name: match[1] ?? `viewport-${index + 1}`, width: Number(match[2]), height: Number(match[3]) };
    });
}

function configuredPaths() {
  const configured = process.env.FORGEFLOW_PERF_WEB_PATHS;
  if (configured === "all") {
    const base = `/app/orgs/${context.organizationID}/workspaces/${context.workspaceID}/projects/${context.projectID}`;
    return [
      "/app",
      `${base}/backlog`,
      `${base}/planning`,
      `${base}/repositories`,
      `${base}/work-items/${context.workItemID}`,
      `${base}/settings/workflow`,
      `${base}/settings/branching`,
      `${base}/settings/autonomous`,
      `${base}/settings/people`,
      `${base}/settings/fields`,
      `${base}/settings/automation`,
      `/app/orgs/${context.organizationID}/settings/general?project_id=${context.projectID}`,
      `/app/orgs/${context.organizationID}/settings/members?project_id=${context.projectID}`,
      `${base.replace(`/projects/${context.projectID}`, "")}/settings/general?project_id=${context.projectID}`,
      `${base.replace(`/projects/${context.projectID}`, "")}/settings/members?project_id=${context.projectID}`,
      "/app/inbox",
      "/app/account/developer",
    ];
  }
  return (configured ?? `/app,/app/orgs/${context.organizationID}/workspaces/${context.workspaceID}/projects/${context.projectID}/backlog`)
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
}

function percentile(values, fraction) {
  if (!values.length) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  const position = (sorted.length - 1) * fraction;
  const lower = Math.floor(position);
  const upper = Math.ceil(position);
  if (lower === upper) return sorted[lower];
  return sorted[lower] + (sorted[upper] - sorted[lower]) * (position - lower);
}

function latencySummary(results) {
  const values = results.filter((result) => Number.isFinite(result.durationMs)).map((result) => result.durationMs);
  return {
    samples: values.length,
    minMs: values.length ? Math.min(...values) : null,
    p50Ms: values.length ? percentile(values, 0.5) : null,
    p95Ms: values.length ? percentile(values, 0.95) : null,
    p99Ms: values.length ? percentile(values, 0.99) : null,
    maxMs: values.length ? Math.max(...values) : null,
  };
}

function statusSummary(results) {
  return results.reduce((summary, result) => {
    const key = result.networkError ? "network_error" : String(result.status);
    summary[key] = (summary[key] ?? 0) + 1;
    return summary;
  }, {});
}

function hasErrorEnvelope(body) {
  try {
    const payload = JSON.parse(body);
    return Boolean(payload && typeof payload.code === "string" && payload.code && typeof payload.request_id === "string" && payload.request_id);
  } catch {
    return false;
  }
}

async function request(url, method = "GET", body = undefined, extraHeaders = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const started = performance.now();
  try {
    const headers = { ...requestHeaders(), ...extraHeaders };
    if (body !== undefined) headers["content-type"] = "application/json";
    const response = await fetch(url, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      redirect: "manual",
      signal: controller.signal,
    });
    const responseBody = await response.text();
    return {
      status: response.status,
      durationMs: performance.now() - started,
      errorEnvelope: response.status >= 400 && hasErrorEnvelope(responseBody),
    };
  } catch (error) {
    return { networkError: error instanceof Error ? error.name : String(error), durationMs: performance.now() - started };
  } finally {
    clearTimeout(timer);
  }
}

function recordResult(result, label, options = {}) {
  if (result.networkError) {
    failures.push(`${label}: ${result.networkError}`);
    return;
  }
  if (result.status === 500) {
    failures.push(`${label}: HTTP 500`);
    return;
  }
  if (result.status >= 500 && !result.errorEnvelope) {
    failures.push(`${label}: unwrapped HTTP ${result.status}`);
    return;
  }
  if (result.status >= 400 && !result.errorEnvelope && result.status !== 429 && !options.allowUnwrappedClientError) {
    failures.push(`${label}: unwrapped HTTP ${result.status}`);
  }
}

async function runSurface() {
  const operations = parseOperations();
  const results = [];
  for (const operation of operations) {
    const url = operationURL(operation);
    const result = await request(url, operation.method, operation.method === "GET" || operation.method === "DELETE" ? undefined : {});
    results.push({ operation: operation.id, method: operation.method, path: url.pathname + url.search, ...result });
    recordResult(result, `${operation.method} ${url.pathname}`, { allowUnwrappedClientError: operation.id === "mcpStreamableHTTP" });
  }
  return {
    operations: operations.length,
    results,
    statuses: statusSummary(results),
    latency: latencySummary(results),
    exactHTTP500: results.filter((result) => result.status === 500).length,
    unwrappedServerErrors: results.filter((result) => result.status >= 500 && !result.errorEnvelope).length,
  };
}

async function runConcurrent(tasks, workerCount) {
  const results = [];
  let cursor = 0;
  async function worker() {
    while (cursor < tasks.length) {
      const index = cursor++;
      results[index] = await tasks[index]();
    }
  }
  await Promise.all(Array.from({ length: Math.min(workerCount, tasks.length) }, worker));
  return results;
}

function processSnapshot(pid) {
  if (!pid) return null;
  try {
    const output = execFileSync("ps", ["-p", String(pid), "-o", "rss=,%cpu=,etime=,time="], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
    const fields = output.split(/\s+/);
    if (fields.length < 2) return null;
    const cpuTimeParts = fields[3]?.split(":").map(Number) ?? [];
    const cpuTimeSeconds = cpuTimeParts.length === 3
      ? cpuTimeParts[0] * 3600 + cpuTimeParts[1] * 60 + cpuTimeParts[2]
      : cpuTimeParts.length === 2
        ? cpuTimeParts[0] * 60 + cpuTimeParts[1]
        : null;
    return { source: "process", pid: Number(pid), rssMB: Number(fields[0]) / 1024, cpuPercent: Number(fields[1]), cpuTimeSeconds, elapsed: fields[2] };
  } catch {
    return null;
  }
}

function dockerSnapshot(container) {
  if (!container) return Promise.resolve(null);
  return new Promise((resolve) => {
    execFile("docker", ["stats", "--no-stream", "--format", "{{.CPUPerc}}\t{{.MemUsage}}", container], { encoding: "utf8" }, (error, stdout) => {
      if (error) {
        resolve(null);
        return;
      }
      const [cpuText, memoryText] = stdout.trim().split("\t");
      const memoryMB = memoryText?.split("/")[0]?.trim();
      const match = memoryMB?.match(/^([\d.]+)\s*(B|KiB|MiB|GiB)$/i);
      if (!match) {
        resolve(null);
        return;
      }
      const units = { b: 1 / 1024 / 1024, kib: 1 / 1024, mib: 1, gib: 1024 };
      resolve({ source: "docker", rssMB: Number(match[1]) * units[match[2].toLowerCase()], cpuPercent: Number.parseFloat(cpuText), container });
    });
  });
}

async function resourceSnapshot(target) {
  if (!target) return null;
  return target.container ? dockerSnapshot(target.container) : processSnapshot(target.pid);
}

function findPidForPort(port) {
  if (!port) return null;
  try {
    const output = execFileSync("lsof", ["-nP", "-t", `-iTCP:${port}`, "-sTCP:LISTEN"], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] });
    return output.split(/\s+/).map(Number).find((pid) => Number.isInteger(pid) && pid > 0) ?? null;
  } catch {
    return null;
  }
}

function configuredProcesses() {
  const apiPort = new URL(apiURL).port;
  const webPort = new URL(webURL).port;
  return {
    api: {
      pid: Number(process.env.FORGEFLOW_PERF_API_PID) || findPidForPort(apiPort),
      container: process.env.FORGEFLOW_PERF_API_CONTAINER,
    },
    web: {
      pid: Number(process.env.FORGEFLOW_PERF_WEB_PID) || findPidForPort(webPort),
      container: process.env.FORGEFLOW_PERF_WEB_CONTAINER,
    },
  };
}

function summarizeResourceSamples(samples) {
  if (!samples.length) return { samples: 0, maxRssMB: null, avgCpuPercent: null, maxCpuPercent: null };
  const rss = samples.map((sample) => sample.rssMB).filter(Number.isFinite);
  const cpu = samples.map((sample) => sample.cpuPercent).filter(Number.isFinite);
  const cpuTimes = samples.map((sample) => sample.cpuTimeSeconds).filter(Number.isFinite);
  return {
    samples: samples.length,
    maxRssMB: rss.length ? Math.max(...rss) : null,
    avgCpuPercent: cpu.length ? cpu.reduce((sum, value) => sum + value, 0) / cpu.length : null,
    maxCpuPercent: cpu.length ? Math.max(...cpu) : null,
    cpuTimeSeconds: cpuTimes.length > 1 ? Math.max(...cpuTimes) - Math.min(...cpuTimes) : null,
    sources: [...new Set(samples.map((sample) => sample.source).filter(Boolean))],
  };
}

async function sampleProcessesDuring(processes, work) {
  const samples = { api: [], web: [] };
  let activeCollection = null;
  const collect = () => {
    if (activeCollection) return activeCollection;
    activeCollection = Promise.all(Object.keys(samples).map(async (name) => {
      const snapshot = await resourceSnapshot(processes[name]);
      if (snapshot) samples[name].push(snapshot);
    })).finally(() => {
      activeCollection = null;
    });
    return activeCollection;
  };
  await collect();
  const timer = setInterval(collect, 100);
  let value;
  try {
    value = await work();
  } finally {
    clearInterval(timer);
    await collect();
  }
  return { value, samples };
}

async function runLoad(processes) {
  const endpoints = ["/health/live", "/api/v1/me", "/api/v1/organizations", "/api/v1/projects"];
  const tasks = Array.from({ length: Math.max(1, iterations) }, (_, iteration) => endpoints.map((endpoint) => async () => {
    const extraHeaders = uniqueLoadRateKeys ? { authorization: `Bearer hard-e2e-load-${iteration}-${endpoint.replaceAll("/", "-")}` } : {};
    const result = await request(new URL(endpoint, `${apiURL}/`), "GET", undefined, extraHeaders);
    return { endpoint, iteration, ...result };
  })).flat();
  const sampled = await sampleProcessesDuring(processes, () => runConcurrent(tasks, Math.max(1, concurrency)));
  const results = sampled.value;
  for (const result of results) {
    if (result.networkError || result.status >= 500 || result.status === 429) recordResult(result, `load ${result.endpoint}`);
  }
  const latency = latencySummary(results);
  if (latency.p95Ms > thresholds.apiP95Ms) failures.push(`API p95 ${latency.p95Ms.toFixed(1)}ms > ${thresholds.apiP95Ms}ms`);
  if (latency.p99Ms > thresholds.apiP99Ms) failures.push(`API p99 ${latency.p99Ms.toFixed(1)}ms > ${thresholds.apiP99Ms}ms`);
  return { iterations, concurrency, endpoints, results, statuses: statusSummary(results), latency, processSamples: sampled.samples };
}

async function runBrowser(processes) {
  const { chromium } = await import("@playwright/test");
  const paths = configuredPaths();
  const repetitions = Number(process.env.FORGEFLOW_PERF_WEB_REPETITIONS ?? "3");
  const viewports = configuredViewports();
  const browser = await chromium.launch({ headless: true, args: ["--enable-precise-memory-info", "--js-flags=--expose-gc"] });
  const browserErrors = [];
  const results = [];
  try {
    const browserContext = await browser.newContext({
      viewport: { width: viewports[0].width, height: viewports[0].height },
      extraHTTPHeaders: requestHeaders(),
    });
    await browserContext.addInitScript(() => {
      window.__forgeflowLongTasks = [];
      if (PerformanceObserver.supportedEntryTypes.includes("longtask")) {
        new PerformanceObserver((list) => {
          window.__forgeflowLongTasks.push(...list.getEntries().map((entry) => entry.duration));
        }).observe({ type: "longtask", buffered: true });
      }
    });
    const page = await browserContext.newPage();
    page.on("response", (response) => {
      if (response.status() >= 500) browserErrors.push(`${response.status()} ${response.request().method()} ${new URL(response.url()).pathname}`);
    });
    for (const viewport of viewports) {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      for (let repetition = 0; repetition < Math.max(1, repetitions); repetition += 1) {
        for (const pathname of paths) {
          if (uniqueBrowserRateKeys) {
            await browserContext.setExtraHTTPHeaders({
              ...requestHeaders(),
              authorization: `Bearer hard-e2e-browser-${viewport.name}-${repetition}-${pathname.replaceAll("/", "-")}`,
            });
          }
          const started = performance.now();
          try {
            const response = await page.goto(`${webURL}${pathname}`, { waitUntil: "domcontentloaded", timeout: timeoutMs });
            await page.waitForTimeout(250);
            const metrics = await page.evaluate(() => {
              globalThis.gc?.();
              const navigation = performance.getEntriesByType("navigation")[0];
              const memory = performance.memory;
              return {
                navigationMs: navigation?.duration ?? null,
                responseStartMs: navigation?.responseStart ?? null,
                domContentLoadedMs: navigation?.domContentLoadedEventEnd ?? null,
                loadEventMs: navigation?.loadEventEnd ?? null,
                heapUsedMB: memory ? memory.usedJSHeapSize / 1024 / 1024 : null,
                longTaskMs: (window.__forgeflowLongTasks ?? []).reduce((sum, duration) => sum + duration, 0),
                horizontalOverflow: Math.max(0, document.documentElement.scrollWidth - document.documentElement.clientWidth),
                clippedInteractive: Array.from(document.querySelectorAll("button, a, input, select, textarea, [role='button']"))
                  .filter((element) => {
                    const style = getComputedStyle(element);
                    const rect = element.getBoundingClientRect();
                    return style.display !== "none" && style.visibility !== "hidden" && Number(style.opacity) !== 0 && rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.left < document.documentElement.clientWidth && (rect.left < -1 || rect.right > document.documentElement.clientWidth + 1);
                  })
                  .slice(0, 10)
                  .map((element) => element.textContent?.trim() || element.getAttribute("aria-label") || element.getAttribute("placeholder") || "unnamed"),
              };
            });
            results.push({ viewport: viewport.name, repetition, pathname, status: response?.status() ?? null, durationMs: performance.now() - started, ...metrics });
            if (response && response.status() >= 500) failures.push(`browser ${viewport.name} ${pathname}: document HTTP ${response.status()}`);
            if (metrics.horizontalOverflow > 1) failures.push(`browser ${viewport.name} ${pathname}: ${metrics.horizontalOverflow}px horizontal overflow`);
            if (metrics.clippedInteractive.length) failures.push(`browser ${viewport.name} ${pathname}: clipped controls ${metrics.clippedInteractive.join(", ")}`);
          } catch (error) {
            const message = error instanceof Error ? error.message.split("\n")[0] : String(error);
            browserErrors.push(`${viewport.name} ${pathname}: ${message}`);
            failures.push(`browser ${viewport.name} ${pathname}: navigation failed`);
          }
        }
      }
    }
    await browserContext.close();
  } finally {
    await browser.close();
  }
  for (const error of browserErrors) failures.push(`browser server error: ${error}`);
  const latency = latencySummary(results);
  if (latency.p95Ms > thresholds.webP95Ms) failures.push(`browser p95 ${latency.p95Ms.toFixed(1)}ms > ${thresholds.webP95Ms}ms`);
  if (latency.p99Ms > thresholds.webP99Ms) failures.push(`browser p99 ${latency.p99Ms.toFixed(1)}ms > ${thresholds.webP99Ms}ms`);
  const heaps = results.map((result) => result.heapUsedMB).filter(Number.isFinite);
  const heapGrowthMB = heaps.length > 1 ? Math.max(...heaps) - Math.min(...heaps) : null;
  if (heapGrowthMB !== null && heapGrowthMB > thresholds.heapGrowthMB) failures.push(`browser heap growth ${heapGrowthMB.toFixed(1)}MB > ${thresholds.heapGrowthMB}MB`);
  return { paths, repetitions, viewports, results, serverErrors: browserErrors, latency, heapGrowthMB };
}

function writeReport(report) {
  const outputDir = path.resolve(root, process.env.FORGEFLOW_PERF_OUTPUT_DIR ?? "artifacts/hard-e2e");
  fs.mkdirSync(outputDir, { recursive: true });
  fs.writeFileSync(path.join(outputDir, "report.json"), `${JSON.stringify(report, null, 2)}\n`);
  const lines = [
    "# Forgeflow hard E2E report",
    "",
    `- Started: ${report.startedAt}`,
    `- API surface: ${report.surface.operations} OpenAPI operations; HTTP 500 = ${report.surface.exactHTTP500}; unwrapped 5xx = ${report.surface.unwrappedServerErrors}`,
    `- API load: ${report.load.latency.samples} samples, p95 ${report.load.latency.p95Ms?.toFixed(1) ?? "n/a"}ms, p99 ${report.load.latency.p99Ms?.toFixed(1) ?? "n/a"}ms`,
    `- Browser: ${report.browser.latency.samples} navigations, p95 ${report.browser.latency.p95Ms?.toFixed(1) ?? "n/a"}ms, p99 ${report.browser.latency.p99Ms?.toFixed(1) ?? "n/a"}ms, heap growth ${report.browser.heapGrowthMB?.toFixed(1) ?? "n/a"}MB`,
    "",
    "## Status counts",
    "",
    `- Surface: ${JSON.stringify(report.surface.statuses)}`,
    `- Load: ${JSON.stringify(report.load.statuses)}`,
    "",
    "## Process resources",
    "",
    `- API: ${JSON.stringify(report.resources.load.api)}`,
    `- Web: ${JSON.stringify(report.resources.browser.web)}`,
    "",
    "## Failures",
    "",
    ...(report.failures.length ? report.failures.map((failure) => `- ${failure}`) : ["- None"]),
    "",
  ];
  fs.writeFileSync(path.join(outputDir, "report.md"), lines.join("\n"));
  return outputDir;
}

async function main() {
  const startedAt = new Date().toISOString();
  const processes = configuredProcesses();
  const load = await runLoad(processes);
  const surface = await runSurface();
  const browserSampled = await sampleProcessesDuring(processes, () => runBrowser(processes));
  const browser = browserSampled.value;
  const report = {
    startedAt,
    config: { apiURL, webURL, iterations, concurrency, timeoutMs, uniqueLoadRateKeys, uniqueBrowserRateKeys, thresholds, processes, hasAuthorization: Boolean(process.env.FORGEFLOW_PERF_AUTHORIZATION) },
    surface,
    load,
    browser,
    resources: {
      load: { api: summarizeResourceSamples(load.processSamples.api) },
      browser: { web: summarizeResourceSamples(browserSampled.samples.web) },
    },
    failures: [...new Set(failures)],
  };
  const outputDir = writeReport(report);
  process.stdout.write(`${JSON.stringify({ outputDir, failures: report.failures, surface: { operations: surface.operations, statuses: surface.statuses }, load: load.latency, browser: browser.latency, resources: report.resources })}\n`);
  if (report.failures.length) process.exitCode = 1;
}

main().catch((error) => {
  failures.push(error instanceof Error ? error.message : String(error));
  const outputDir = writeReport({ startedAt: new Date().toISOString(), surface: { operations: 0, exactHTTP500: 0, unwrappedServerErrors: 0, statuses: {} }, load: { latency: { samples: 0, p95Ms: null, p99Ms: null }, statuses: {} }, browser: { latency: { samples: 0, p95Ms: null, p99Ms: null }, heapGrowthMB: null }, resources: { load: { api: {} }, browser: { web: {} } }, failures });
  process.stdout.write(`${JSON.stringify({ outputDir, failures })}\n`);
  process.exitCode = 1;
});
