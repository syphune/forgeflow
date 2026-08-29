#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { performance } from "node:perf_hooks";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const apiURL = (process.env.FORGEFLOW_FUNCTIONAL_API_URL ?? "http://127.0.0.1:18082").replace(/\/+$/, "");
const ids = {
  organization: process.env.FORGEFLOW_FUNCTIONAL_ORGANIZATION_ID ?? "582554ef-120d-44cf-89cb-2db3b959d689",
  actor: process.env.FORGEFLOW_FUNCTIONAL_ACTOR_ID ?? "37aa4a1b-e5e1-40cc-b036-23e0922f0ba5",
  workspace: process.env.FORGEFLOW_FUNCTIONAL_WORKSPACE_ID ?? "1e3f9209-e7aa-412e-a701-602a9693d5c1",
  project: process.env.FORGEFLOW_FUNCTIONAL_PROJECT_ID ?? "e7a275e7-ad39-480e-af26-f1d60278be8e",
  repository: process.env.FORGEFLOW_FUNCTIONAL_REPOSITORY_ID ?? "33b62aea-16ee-40c4-89f8-8902184100af",
  existingWorkItem: process.env.FORGEFLOW_FUNCTIONAL_EXISTING_WORK_ITEM_ID ?? "475e73eb-5309-4ec8-b5db-01553634d8bf",
  relatedWorkItem: process.env.FORGEFLOW_FUNCTIONAL_RELATED_WORK_ITEM_ID ?? "c4a6354c-2f11-4c73-b44d-35fc90ba7ee0",
  memberLogin: process.env.FORGEFLOW_FUNCTIONAL_MEMBER_LOGIN ?? "smoke-user",
};
const timeoutMs = Number(process.env.FORGEFLOW_FUNCTIONAL_TIMEOUT_MS ?? "15000");
const runID = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
const agentPrompt = "Fix Astro relationship prose so ordinary Anh/Chị stays unchanged while structured role mapping remains explicit; run the verified desktop and mobile regression cases.";
const outcomes = [];
const failures = [];
let requestNumber = 0;

function endpoint(pathname) {
  return new URL(pathname, `${apiURL}/`);
}

function headers(extra = {}) {
  return {
    accept: "application/json",
    authorization: `Bearer forgeflow-functional-${runID}-${requestNumber++}`,
    "x-organization-id": ids.organization,
    "x-actor-id": ids.actor,
    "x-project-id": ids.project,
    ...extra,
  };
}

function hasErrorEnvelope(body) {
  try {
    const value = JSON.parse(body);
    return Boolean(value && typeof value.code === "string" && value.code && typeof value.request_id === "string" && value.request_id);
  } catch {
    return false;
  }
}

async function call(name, method, pathname, body, expected = [200], options = {}) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const started = performance.now();
  const requestHeaders = headers(options.headers);
  const isForm = typeof FormData !== "undefined" && body instanceof FormData;
  if (body !== undefined && !isForm) requestHeaders["content-type"] = "application/json";
  try {
    const response = await fetch(endpoint(pathname), {
      method,
      headers: requestHeaders,
      body: body === undefined ? undefined : isForm ? body : JSON.stringify(body),
      redirect: "manual",
      signal: controller.signal,
    });
    const text = await response.text();
    let payload = null;
    try {
      payload = text ? JSON.parse(text) : null;
    } catch {
      payload = null;
    }
    const result = {
      name,
      method,
      path: new URL(pathname, `${apiURL}/`).pathname,
      status: response.status,
      durationMs: performance.now() - started,
      errorEnvelope: response.status >= 400 && hasErrorEnvelope(text),
    };
    outcomes.push(result);
    if (response.status >= 500) failures.push(`${name}: HTTP ${response.status}`);
    if (!expected.includes(response.status)) failures.push(`${name}: expected ${expected.join("/")}, got ${response.status}`);
    return { response, payload, text, result };
  } catch (error) {
    const result = { name, method, path: new URL(pathname, `${apiURL}/`).pathname, networkError: error instanceof Error ? error.name : String(error), durationMs: performance.now() - started };
    outcomes.push(result);
    failures.push(`${name}: ${result.networkError}`);
    return { response: null, payload: null, text: "", result };
  } finally {
    clearTimeout(timer);
  }
}

function value(result, name) {
  const current = result?.payload?.[name];
  if (current === undefined || current === null || current === "") throw new Error(`missing ${name}`);
  return current;
}

function assertThat(label, condition) {
  if (!condition) failures.push(label);
}

async function main() {
  let workItemID;
  let workItemVersion;
  let attachmentID;
  let commentID;
  let linkID;
  let labelID;
  let customFieldID;
  let automationID;
  let sprintID;
  let tokenID;
  let memberID;
  let environmentID;
  let deploymentID;
  let agentRunID;
  let cancelRunID;
  let autonomousRunID;
  let autonomousChildID;

  try {
    await call("health live", "GET", "/health/live", undefined, [200]);
    await call("health ready", "GET", "/health/ready", undefined, [200]);
    await call("get me", "GET", "/api/v1/me", undefined, [200]);
    await call("list organizations", "GET", "/api/v1/organizations", undefined, [200]);
    await call("get organization", "GET", "/api/v1/organizations/current", undefined, [200]);
    await call("organization authorization", "GET", `/api/v1/organizations/${ids.organization}/authorization`, undefined, [200]);
    await call("list organization members", "GET", "/api/v1/organizations/current/members", undefined, [200]);
    await call("list workspaces", "GET", "/api/v1/workspaces", undefined, [200]);
    await call("workspace authorization", "GET", `/api/v1/workspaces/${ids.workspace}/authorization`, undefined, [200]);
    await call("list projects", "GET", `/api/v1/projects?workspace_id=${ids.workspace}`, undefined, [200]);
    await call("project authorization", "GET", `/api/v1/projects/${ids.project}/authorization`, undefined, [200]);
    await call("list project members", "GET", `/api/v1/projects/${ids.project}/members`, undefined, [200]);

    const workflow = (await call("get workflow", "GET", "/api/v1/workflows/current", undefined, [200])).payload;
    await call("list workflow statuses", "GET", "/api/v1/workflows/current/statuses", undefined, [200]);
    await call("list workflow transitions", "GET", "/api/v1/workflows/current/transitions", undefined, [200]);
    await call("save workflow round trip", "PUT", "/api/v1/workflows/current", {
      name: workflow.name,
      statuses: workflow.statuses,
      transitions: workflow.transitions,
    }, [200]);
    await call("get board", "GET", "/api/v1/boards/current", undefined, [200]);

    await call("list GitHub installations", "GET", "/api/v1/integrations/github/installations", undefined, [200]);
    await call("list GitHub repositories", "GET", `/api/v1/integrations/github/repositories?project_id=${ids.project}`, undefined, [200]);
    await call("list linked repositories", "GET", `/api/v1/projects/${ids.project}/repositories`, undefined, [200]);
    await call("repository context", "GET", `/api/v1/projects/${ids.project}/repositories/${ids.repository}/context`, undefined, [200]);
    await call("repository tree", "GET", `/api/v1/projects/${ids.project}/repositories/${ids.repository}/tree`, undefined, [200]);
    await call("repository file", "GET", `/api/v1/projects/${ids.project}/repositories/${ids.repository}/file?path=README.md`, undefined, [200]);
    const snapshots = (await call("repository snapshots", "GET", `/api/v1/projects/${ids.project}/repositories/${ids.repository}/snapshots?limit=10`, undefined, [200])).payload;
    if (snapshots?.items?.[0]?.id) {
      const snapshotID = snapshots.items[0].id;
      await call("repository snapshot", "GET", `/api/v1/projects/${ids.project}/repositories/${ids.repository}/snapshots/${snapshotID}`, undefined, [200]);
      await call("repository snapshot symbols", "GET", `/api/v1/projects/${ids.project}/repositories/${ids.repository}/snapshots/${snapshotID}/symbols?limit=10`, undefined, [200]);
    }

    const created = await call("create Astro regression bug", "POST", "/api/v1/work-items", {
      project_id: ids.project,
      type: "BUG",
      title: `Astro pronoun hard E2E ${runID}`,
      description: "Ordinary Anh/Chị in relationship prose must never be broad-replaced with Người ấy.",
      repository_id: ids.repository,
      priority: "HIGH",
    }, [201], { headers: { "Idempotency-Key": `functional-${runID}` } });
    workItemID = value(created, "id");
    workItemVersion = value(created, "version");
    const fetched = await call("get created work item", "GET", `/api/v1/work-items/${workItemID}`, undefined, [200]);
    workItemVersion = value(fetched, "version");
    const updatedItem = await call("update work item", "PATCH", `/api/v1/work-items/${workItemID}`, {
      title: `Astro pronoun hard E2E ${runID} confirmed`,
      description: "Confirmed regression: preserve Anh/Chị in ordinary prose; keep structured role mapping explicit.",
      expected_version: workItemVersion,
    }, [200]);
    workItemVersion = value(updatedItem, "version");
    const refining = await call("start refining", "POST", `/api/v1/work-items/${workItemID}/transitions`, { transition_key: "start_refining", expected_version: workItemVersion }, [200]);
    workItemVersion = value(refining, "version");

    const evidence = new FormData();
    evidence.append("file", new Blob(["Astro evidence: Anh remains Anh on desktop and mobile.\n"], { type: "text/plain" }), "astro-pronoun-evidence.txt");
    const uploaded = await call("upload evidence attachment", "POST", `/api/v1/work-items/${workItemID}/attachments`, evidence, [201]);
    attachmentID = value(uploaded, "id");
    await call("list attachments", "GET", `/api/v1/work-items/${workItemID}/attachments`, undefined, [200]);
    const downloaded = await call("download evidence attachment", "GET", `/api/v1/work-items/${workItemID}/attachments/${attachmentID}`, undefined, [200]);
    assertThat("attachment download has content", downloaded.text.length > 0);

    const proposal = await call("create AI specification proposal", "POST", `/api/v1/work-items/${workItemID}/specification/proposals`, { field: "ACTUAL_BEHAVIOR", value: "A structured role token is mapped without touching prose.", provenance: "AI_HYPOTHESIS" }, [201]);
    const proposalID = value(proposal, "id");
    await call("list specification proposals", "GET", `/api/v1/work-items/${workItemID}/specification/proposals`, undefined, [200]);
    await call("reject AI proposal", "POST", `/api/v1/work-items/${workItemID}/specification/proposals/${proposalID}/reject`, undefined, [200]);
    const analysis = await call("create AI analysis", "POST", `/api/v1/work-items/${workItemID}/analyses`, {
      root_cause_hypothesis: "A broad textual replace is not constrained to structured role tokens.",
      blast_radius: "Relationship interpretation prose across desktop and mobile.",
      implementation_plan: "Scope replacement to structured tokens and add prose regression assertions.",
      test_plan: "Run the Astro relationship suite at desktop and 390px viewport.",
      evidence_refs: [attachmentID],
      confidence: 0.93,
    }, [201]);
    assertThat("analysis was recorded as AI hypothesis", analysis.payload?.provenance === "AI_HYPOTHESIS");
    await call("list analyses", "GET", `/api/v1/work-items/${workItemID}/analyses`, undefined, [200]);

    const beforeSpec = (await call("get initial specification", "GET", `/api/v1/work-items/${workItemID}/specification`, undefined, [200])).payload;
    const updatedSpec = await call("write human specification", "PATCH", `/api/v1/work-items/${workItemID}/specification`, {
      expected_version: beforeSpec.specification.version,
      summary: "Keep ordinary relationship prose pronouns intact; only structured role tokens may be mapped.",
      fields: {
        PROBLEM_STATEMENT: "The broad replace changes Anh into Người ấy in relationship interpretations.",
        EXPECTED_BEHAVIOR: "Ordinary prose keeps Anh and Chị unchanged.",
        ACTUAL_BEHAVIOR: "The broad-match replacement rewrites natural pronouns.",
        ENVIRONMENT: "Production web app on desktop and mobile.",
      },
      reproduction_steps: [{
        action: "Open an Astro relationship interpretation containing ordinary Anh and Chị prose.",
        expected_result: "The prose shows Anh and Chị exactly as written.",
        observed_result: "Before the fix, Anh becomes Người ấy; after the fix, it stays Anh.",
        evidence_refs: [attachmentID],
      }],
      acceptance_criteria: [{ statement: "No ordinary Anh/Chị prose is replaced; structured role mapping remains explicit." }],
      regression_test_cases: [{ scenario: "Render desktop and mobile relationship prose with Anh and Chị.", expected_result: "Both viewports preserve the natural pronouns and have no horizontal overflow." }],
      context_refs: [{ repository_id: ids.repository, module: "apps/web", file: "README.md", symbol: "relationship prose", rationale: "Linked repository context for the report-rendering path." }],
      repository_id: ids.repository,
    }, [200]);
    value(updatedSpec, "version");
    for (const field of ["PROBLEM_STATEMENT", "EXPECTED_BEHAVIOR", "ACTUAL_BEHAVIOR", "ENVIRONMENT"]) {
      await call(`verify field ${field}`, "POST", `/api/v1/work-items/${workItemID}/specification/verifications`, { kind: "field", field }, [204]);
    }
    await call("verify reproduction step", "POST", `/api/v1/work-items/${workItemID}/specification/verifications`, { kind: "reproduction_step", position: 1 }, [204]);
    await call("verify acceptance criterion", "POST", `/api/v1/work-items/${workItemID}/specification/verifications`, { kind: "acceptance_criterion", position: 1 }, [204]);
    await call("verify regression case", "POST", `/api/v1/work-items/${workItemID}/specification/verifications`, { kind: "regression_case", position: 1 }, [204]);
    const readyBeforeReviewResult = await call("check readiness before review", "GET", `/api/v1/work-items/${workItemID}/specification`, undefined, [200]);
    const readyBeforeReview = readyBeforeReviewResult.payload;
    assertThat("verified spec waits for human review", readyBeforeReview.readiness?.ready === false && readyBeforeReview.readiness?.reviewed === false);
    const reviewState = await call("request specification review", "POST", `/api/v1/work-items/${workItemID}/transitions`, { transition_key: "request_review", expected_version: value((await call("refresh work item before review", "GET", `/api/v1/work-items/${workItemID}`, undefined, [200])), "version") }, [200]);
    workItemVersion = value(reviewState, "version");
    await call("human review specification", "POST", `/api/v1/work-items/${workItemID}/specification/review`, { expected_version: readyBeforeReview.specification.version }, [200]);
    await call("list specification versions", "GET", `/api/v1/work-items/${workItemID}/specification/versions?limit=20`, undefined, [200]);
    const readyState = await call("mark work item ready", "POST", `/api/v1/work-items/${workItemID}/transitions`, { transition_key: "mark_ready", expected_version: workItemVersion }, [200]);
    workItemVersion = value(readyState, "version");
    assertThat("work item reaches READY only after review", readyState.payload?.status === "READY");

    const comment = await call("create comment", "POST", `/api/v1/work-items/${workItemID}/comments`, { body: "Agent prompt and human acceptance agree: preserve Anh/Chị in ordinary prose." }, [201]);
    commentID = value(comment, "id");
    await call("list comments", "GET", `/api/v1/work-items/${workItemID}/comments`, undefined, [200]);
    await call("update comment", "PATCH", `/api/v1/work-items/${workItemID}/comments/${commentID}`, { body: "QA evidence confirms Anh/Chị are preserved in ordinary prose." }, [200]);
    const label = await call("add label", "POST", `/api/v1/work-items/${workItemID}/labels`, { name: `e2e-${runID.slice(-8)}`, color: "#e67e22" }, [201]);
    labelID = value(label, "id");
    await call("list labels", "GET", `/api/v1/work-items/${workItemID}/labels`, undefined, [200]);
    const link = await call("link related Astro issue", "POST", `/api/v1/work-items/${workItemID}/links`, { target_id: ids.relatedWorkItem, relation_type: "relates_to" }, [201]);
    linkID = value(link, "id");
    await call("list work item links", "GET", `/api/v1/work-items/${workItemID}/links`, undefined, [200]);

    const field = await call("create custom field", "POST", `/api/v1/projects/${ids.project}/custom-fields`, { key: `E2E_${runID.replace(/[^a-z0-9]/gi, "").slice(-20).toUpperCase()}`, display_name: "E2E severity", value_type: "SELECT", options: ["low", "high"], required: false }, [201]);
    customFieldID = value(field, "id");
    await call("list custom fields", "GET", `/api/v1/projects/${ids.project}/custom-fields`, undefined, [200]);
    await call("update custom field", "PATCH", `/api/v1/projects/${ids.project}/custom-fields/${customFieldID}`, { display_name: "E2E severity updated" }, [200]);
    await call("set custom field value", "PUT", `/api/v1/work-items/${workItemID}/custom-fields/${customFieldID}?project_id=${ids.project}`, { value: "high" }, [200]);
    await call("list custom field values", "GET", `/api/v1/work-items/${workItemID}/custom-fields?project_id=${ids.project}`, undefined, [200]);

    const sprint = await call("create sprint", "POST", "/api/v1/sprints", { project_id: ids.project, name: `Hard E2E ${runID}`, goal: "Verify the Astro release workflow." }, [201]);
    sprintID = value(sprint, "id");
    await call("list sprints", "GET", "/api/v1/sprints", undefined, [200]);
    await call("update sprint", "PATCH", `/api/v1/sprints/${sprintID}`, { name: `Hard E2E updated ${runID}`, goal: "Keep the workflow usable." }, [200]);
    await call("start sprint", "POST", `/api/v1/sprints/${sprintID}/start`, undefined, [200]);
    await call("complete sprint", "POST", `/api/v1/sprints/${sprintID}/complete`, undefined, [200]);
    sprintID = undefined;
    const deletableSprint = await call("create planned sprint for deletion", "POST", "/api/v1/sprints", { project_id: ids.project, name: `Hard E2E cleanup ${runID}`, goal: "Exercise planned sprint deletion." }, [201]);
    sprintID = value(deletableSprint, "id");
    await call("delete planned sprint", "DELETE", `/api/v1/sprints/${sprintID}`, undefined, [204]);
    sprintID = undefined;

    const rule = await call("create automation rule", "POST", `/api/v1/projects/${ids.project}/automation-rules`, { name: `E2E notify ${runID}`, event_type: "work_item.transitioned", action_type: "notify", config: { title: "Astro E2E transition", body: "{event_type}: {aggregate_id}" } }, [201]);
    automationID = value(rule, "id");
    await call("list automation rules", "GET", `/api/v1/projects/${ids.project}/automation-rules`, undefined, [200]);
    await call("toggle automation rule", "PATCH", `/api/v1/projects/${ids.project}/automation-rules/${automationID}`, { enabled: false }, [200]);

    const policy = (await call("get AI policy", "GET", `/api/v1/projects/${ids.project}/ai-policy`, undefined, [200])).payload;
    await call("save AI policy round trip", "PUT", `/api/v1/projects/${ids.project}/ai-policy`, policy, [200]);
    const environment = await call("create staging environment", "POST", `/api/v1/projects/${ids.project}/environments`, { key: `e2e-${runID.slice(-10)}`, name: "E2E staging", kind: "staging", require_approval: true, auto_deploy: false, metadata: { purpose: "functional-e2e" } }, [201]);
    environmentID = value(environment, "id");
    await call("list environments", "GET", `/api/v1/projects/${ids.project}/environments`, undefined, [200]);
    const deployment = await call("request deployment", "POST", "/api/v1/deployments", { project_id: ids.project, environment_id: environmentID, commit_sha: "fa3a5d2e3c7ddd59484cbf26451742d7151bc45a" }, [201]);
    deploymentID = value(deployment, "id");
    await call("get deployment", "GET", `/api/v1/deployments/${deploymentID}`, undefined, [200]);
    await call("approve deployment", "POST", `/api/v1/deployments/${deploymentID}/approve`, undefined, [200]);
    await call("deployment running callback", "POST", `/api/v1/deployments/${deploymentID}/status`, { status: "RUNNING", external_id: `e2e-${runID}` }, [200]);
    await call("deployment success callback", "POST", `/api/v1/deployments/${deploymentID}/status`, { status: "SUCCEEDED", url: "https://example.invalid/forgeflow-e2e" }, [200]);

    const agent = await call("create AgentRun", "POST", "/api/v1/agent-runs", {
      project_id: ids.project,
      work_item_id: ids.existingWorkItem,
      repository_id: ids.repository,
      agent_provider: "codex",
      agent_name: `Astro hard E2E agent ${runID}`,
      model: "e2e-model",
      base_sha: "fa3a5d2e3c7ddd59484cbf26451742d7151bc45a",
      branch: `forgeflow/e2e/${runID}`,
      execution_inputs: { prompt: agentPrompt, test_case_positions: [1], tool_permissions: ["repository.read"], mcp_permissions: ["work_item.get_context"], execution_profile: "default" },
      execution_policy: { runtime: "server", network: "disabled" },
    }, [201]);
    agentRunID = value(agent, "id");
    await call("get AgentRun", "GET", `/api/v1/agent-runs/${agentRunID}`, undefined, [200]);
    await call("approve AgentRun", "POST", `/api/v1/agent-runs/${agentRunID}/approve`, undefined, [200]);
    await call("start AgentRun", "POST", `/api/v1/agent-runs/${agentRunID}/start`, undefined, [200]);
    await call("AgentRun heartbeat", "POST", `/api/v1/agent-runs/${agentRunID}/heartbeat`, undefined, [200]);
    for (const status of ["PLANNING", "INVESTIGATING", "IMPLEMENTING", "TESTING"]) {
      await call(`AgentRun transition ${status}`, "POST", `/api/v1/agent-runs/${agentRunID}/transition`, { status }, [200]);
    }
    await call("record AgentRun test results", "POST", `/api/v1/agent-runs/${agentRunID}/test-results`, { test_cases: [{ position: 1, status: "PASS", note: "Desktop and mobile prose assertions passed.", evidence_refs: [attachmentID] }], review_note: "Human QA reviewed the regression evidence." }, [200]);
    await call("AgentRun attach result", "POST", `/api/v1/agent-runs/${agentRunID}/result`, { commit_sha: "fa3a5d2e3c7ddd59484cbf26451742d7151bc45a", result: { summary: "Pronoun replacement constrained to structured roles." }, metadata: { source: "functional-e2e" } }, [200]);
    await call("AgentRun transition reviewing", "POST", `/api/v1/agent-runs/${agentRunID}/transition`, { status: "REVIEWING" }, [200]);
    await call("AgentRun attach step", "POST", `/api/v1/agent-runs/${agentRunID}/steps`, { sequence: 1, phase: "TESTING", status: "PASS", summary: "Ran Astro desktop and mobile relationship regression.", files_read: 2, files_modified: 1, commands: ["pnpm test"], tests: ["relationship prose regression"] }, [201]);
    await call("AgentRun attach artifact", "POST", `/api/v1/agent-runs/${agentRunID}/artifacts`, { artifact_type: "evidence", name: "astro-pronoun-report.json", size_bytes: 128, content_hash: "e2e" }, [201]);
    await call("complete AgentRun", "POST", `/api/v1/agent-runs/${agentRunID}/transition`, { status: "COMPLETED" }, [200]);

    const cancelAgent = await call("create cancellable AgentRun", "POST", "/api/v1/agent-runs", {
      project_id: ids.project, work_item_id: ids.existingWorkItem, repository_id: ids.repository, agent_provider: "codex", agent_name: `Astro cancellation ${runID}`, execution_inputs: { prompt: agentPrompt, test_case_positions: [1] }, execution_policy: { runtime: "server" },
    }, [201]);
    cancelRunID = value(cancelAgent, "id");
    await call("approve cancellable AgentRun", "POST", `/api/v1/agent-runs/${cancelRunID}/approve`, undefined, [200]);
    await call("start cancellable AgentRun", "POST", `/api/v1/agent-runs/${cancelRunID}/start`, undefined, [200]);
    const cancelledAgent = await call("cancel AgentRun", "POST", `/api/v1/agent-runs/${cancelRunID}/cancel`, undefined, [200]);
    if (cancelledAgent.result.status === 200) cancelRunID = undefined;
    await call("list AgentRuns", "GET", `/api/v1/agent-runs?work_item_id=${ids.existingWorkItem}`, undefined, [200]);

    const autonomous = await call("start autonomous workflow", "POST", "/api/v1/autonomous-runs", {
      project_id: ids.project, work_item_id: ids.existingWorkItem, repository_id: ids.repository, objective: agentPrompt, agent_provider: "codex", agent_name: `Astro autonomous ${runID}`, model: "e2e-model", test_case_positions: [1],
    }, [201]);
    autonomousRunID = value(autonomous, "id");
    autonomousChildID = value(autonomous, "current_agent_run_id");
    await call("get autonomous workflow", "GET", `/api/v1/autonomous-runs/${autonomousRunID}`, undefined, [200]);
    for (const status of ["PLANNING", "INVESTIGATING", "IMPLEMENTING", "TESTING"]) {
      await call(`autonomous child transition ${status}`, "POST", `/api/v1/agent-runs/${autonomousChildID}/transition`, { status }, [200]);
    }
    await call("record autonomous failed test", "POST", `/api/v1/autonomous-runs/${autonomousRunID}/test-results`, { test_cases: [{ position: 1, status: "FAIL", note: "Mobile assertion needs a rerun." }], review_note: "Rerun the mobile viewport before submitting." }, [200]);
    await call("get autonomous feedback", "GET", `/api/v1/autonomous-runs/${autonomousRunID}`, undefined, [200]);
    await call("add autonomous feedback", "POST", `/api/v1/autonomous-runs/${autonomousRunID}/feedback`, { source: "human", note: "Please rerun the unresolved mobile case only.", test_case_positions: [1] }, [201]);
    const retried = await call("retry autonomous unresolved case", "POST", `/api/v1/autonomous-runs/${autonomousRunID}/retry`, { feedback: "Rerun only case 1 after the mobile fix.", test_case_positions: [1] }, [200]);
    autonomousChildID = value(retried, "current_agent_run_id");
    const cancelledAutonomous = await call("cancel autonomous workflow", "POST", `/api/v1/autonomous-runs/${autonomousRunID}/cancel`, undefined, [200]);
    if (cancelledAutonomous.result.status === 200) autonomousRunID = undefined;
    await call("list autonomous workflows", "GET", `/api/v1/autonomous-runs?work_item_id=${ids.existingWorkItem}`, undefined, [200]);

    const member = await call("add organization member", "POST", "/api/v1/organizations/current/members", { login: ids.memberLogin, role_key: "developer" }, [201]);
    memberID = value(member, "id");
    await call("set organization member role", "PUT", `/api/v1/organizations/current/members/${memberID}`, { role_key: "qa" }, [200]);
    const removedMember = await call("remove organization member", "DELETE", `/api/v1/organizations/current/members/${memberID}`, undefined, [204]);
    if (removedMember.result.status === 204) memberID = undefined;
    const token = await call("create personal access token", "POST", "/api/v1/me/tokens", { name: `functional-e2e-${runID}`, scopes: ["project.read"], expires_in_days: 7 }, [201]);
    tokenID = value(token, "id");
    await call("list personal access tokens", "GET", "/api/v1/me/tokens", undefined, [200]);
    const revokedToken = await call("revoke personal access token", "DELETE", `/api/v1/me/tokens/${tokenID}`, undefined, [204]);
    if (revokedToken.result.status === 204) tokenID = undefined;

    await call("list notifications", "GET", "/api/v1/notifications?limit=10", undefined, [200]);
    await call("unread notification count", "GET", "/api/v1/notifications/unread-count", undefined, [200]);
    await call("mark all notifications read", "POST", "/api/v1/notifications/read-all", undefined, [204]);
    await call("list audit records", "GET", "/api/v1/audit?limit=10", undefined, [200]);

    for (const [name, body] of [
      ["MCP initialize", { jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "forgeflow-functional-e2e", version: "1" } } }],
      ["MCP tools list", { jsonrpc: "2.0", id: 2, method: "tools/list", params: {} }],
      ["MCP resources list", { jsonrpc: "2.0", id: 3, method: "resources/list", params: {} }],
      ["MCP templates list", { jsonrpc: "2.0", id: 4, method: "resources/templates/list", params: {} }],
      ["MCP project list", { jsonrpc: "2.0", id: 5, method: "tools/call", params: { name: "project.list", arguments: {} } }],
    ]) {
      await call(name, "POST", `/api/v1/mcp?project_id=${ids.project}`, body, [200], { headers: { accept: "application/json, text/event-stream" } });
    }
  } catch (error) {
    failures.push(error instanceof Error ? error.message : String(error));
  } finally {
    if (tokenID) await call("cleanup token", "DELETE", `/api/v1/me/tokens/${tokenID}`, undefined, [204]);
    if (memberID) await call("cleanup organization member", "DELETE", `/api/v1/organizations/current/members/${memberID}`, undefined, [204]);
    if (autonomousRunID) await call("cleanup autonomous workflow", "POST", `/api/v1/autonomous-runs/${autonomousRunID}/cancel`, undefined, [200]);
    if (cancelRunID) await call("cleanup cancelled AgentRun", "POST", `/api/v1/agent-runs/${cancelRunID}/cancel`, undefined, [200]);
    if (automationID) await call("cleanup automation rule", "DELETE", `/api/v1/projects/${ids.project}/automation-rules/${automationID}`, undefined, [204]);
    if (customFieldID) {
      await call("cleanup custom field value", "PUT", `/api/v1/work-items/${workItemID}/custom-fields/${customFieldID}?project_id=${ids.project}`, { value: null }, [200]);
      await call("cleanup custom field", "DELETE", `/api/v1/projects/${ids.project}/custom-fields/${customFieldID}`, undefined, [204]);
    }
    if (sprintID) await call("cleanup sprint", "DELETE", `/api/v1/sprints/${sprintID}`, undefined, [204]);
    if (linkID) await call("cleanup work item link", "DELETE", `/api/v1/work-items/${workItemID}/links/${linkID}`, undefined, [204]);
    if (labelID) await call("cleanup work item label", "DELETE", `/api/v1/work-items/${workItemID}/labels/${labelID}`, undefined, [204]);
    if (commentID) await call("cleanup comment", "DELETE", `/api/v1/work-items/${workItemID}/comments/${commentID}`, undefined, [200]);
    if (attachmentID) await call("cleanup attachment", "DELETE", `/api/v1/work-items/${workItemID}/attachments/${attachmentID}`, undefined, [204]);
    if (workItemID) {
      const current = await call("read item before archive", "GET", `/api/v1/work-items/${workItemID}`, undefined, [200]);
      if (current.payload?.version) await call("archive functional fixture", "DELETE", `/api/v1/work-items/${workItemID}?expected_version=${current.payload.version}`, undefined, [204]);
    }
  }

  const outputDir = path.join(root, process.env.FORGEFLOW_FUNCTIONAL_OUTPUT_DIR ?? "artifacts/functional-e2e-astro");
  fs.mkdirSync(outputDir, { recursive: true });
  const statusCounts = outcomes.reduce((summary, result) => {
    const key = result.networkError ? "network_error" : String(result.status);
    summary[key] = (summary[key] ?? 0) + 1;
    return summary;
  }, {});
  const report = {
    startedAt: new Date().toISOString(),
    apiURL,
    runID,
    scenario: "Astro pronoun replacement production-readiness journey",
    agentPrompt,
    fixture: { workItemID, existingWorkItem: ids.existingWorkItem, autonomousRunID, agentRunID, deploymentID, environmentID },
    calls: outcomes.length,
    statuses: statusCounts,
    exactHTTP500: outcomes.filter((result) => result.status === 500).length,
    serverErrors: outcomes.filter((result) => result.status >= 500).length,
    networkErrors: outcomes.filter((result) => result.networkError).length,
    failures: [...new Set(failures)],
    operations: outcomes,
  };
  fs.writeFileSync(path.join(outputDir, "report.json"), `${JSON.stringify(report, null, 2)}\n`);
  fs.writeFileSync(path.join(outputDir, "report.md"), [
    "# Forgeflow functional E2E report",
    "",
    `- Scenario: ${report.scenario}`,
    `- Calls: ${report.calls}`,
    `- Statuses: ${JSON.stringify(report.statuses)}`,
    `- HTTP 500: ${report.exactHTTP500}; any 5xx: ${report.serverErrors}; network errors: ${report.networkErrors}`,
    `- Agent prompt: ${agentPrompt}`,
    "",
    "## Journey",
    "",
    "- Tenant selection and authorization",
    "- Repository context and snapshot reads",
    "- Astro BUG intake, human specification, evidence, verification, review, and READY gate",
    "- AI proposal/analysis provenance, comments, labels, links, custom fields",
    "- Sprint, automation, AI policy, environment, deployment approval/status",
    "- AgentRun approval, execution phases, test evidence, result, artifact, completion, cancellation",
    "- Autonomous execution, test failure feedback, unresolved-case retry, cancellation",
    "- Organization member and PAT lifecycle, notifications, audit, MCP HTTP handshake/tool call",
    "",
    "## Failures",
    "",
    ...(report.failures.length ? report.failures.map((failure) => `- ${failure}`) : ["- None"]),
    "",
  ].join("\n"));
  process.stdout.write(`${JSON.stringify({ outputDir, calls: report.calls, statuses: report.statuses, failures: report.failures })}\n`);
  if (report.failures.length) process.exitCode = 1;
}

main().catch((error) => {
  process.stderr.write(`${error instanceof Error ? error.stack : String(error)}\n`);
  process.exitCode = 1;
});
