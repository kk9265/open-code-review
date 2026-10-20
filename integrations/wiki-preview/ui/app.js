// SPDX-License-Identifier: Apache-2.0
"use strict";

let copy = {};
let pages = [];
let selectedKey = null;
let category = "all";
let revision = 0;
const $ = (id) => document.getElementById(id);
const t = (key) => copy[key] || key;
const categories = ["direct", "indirect", "parent", "unknown", "unchanged"];

function el(tag, text, className) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
}

function badge(kind) { return el("span", t(kind), "badge " + kind); }
function short(value) { return value ? value.slice(0, 12) : t("initial"); }
function code(value) { return el("code", value); }
function message(value, error = false) {
  $("message").textContent = value;
  $("message").classList.toggle("error", error);
  $("message").hidden = !value;
}

async function get(url) {
  const response = await fetch((document.documentElement.dataset.wikiBase || "") + url, { cache: "no-store" });
  if (!response.ok) throw new Error("Preview unavailable");
  return response.json();
}

function normalize(data) {
  const catalog = new Map(data.catalog.map((page) => [page.key, page]));
  const result = new Map();
  const add = (key, kind, extra = {}) => result.set(key, {
    key, title: catalog.get(key)?.title || key,
    sources: catalog.get(key)?.source_files || [], kind, ...extra,
  });
  for (const page of data.preview.affected_pages || []) {
    const legacyKind = page.reasons?.every((reason) => reason.startsWith("child_affected:")) ? "parent" : "direct";
    add(page.key, page.impact || legacyKind, page);
  }
  for (const key of data.preview.potentially_affected_pages || []) add(key, "unknown");
  for (const key of data.preview.unchanged_pages || []) add(key, "unchanged");
  return [...result.values()].sort((a, b) =>
    categories.indexOf(a.kind) - categories.indexOf(b.kind) || a.key.localeCompare(b.key));
}

function renderGraphs(graphs) {
  $("graphs").replaceChildren();
  for (const side of ["baseline", "target"]) {
    const graph = graphs?.[side] || { status: "unavailable", reason: "not_supplied" };
    const card = el("div", undefined, "graph-card");
    card.append(el("strong", t(side)), badge(graph.status),
      code(graph.commit || t("initial")));
    if (graph.status === "available") {
      card.append(el("p", t("indexed") + ": " + graph.indexed_file_count));
    } else card.append(el("p", t(graph.reason || "unavailable")));
    if (graph.detail) card.append(el("p", graph.detail, "muted"));
    const missing = graph.unindexed_changed_files || [];
    if (missing.length) card.append(el("p", t("unindexed") + ": " + missing.join(", ")));
    card.append(el("p", t("graphLimit"), "muted"));
    $("graphs").append(card);
  }
}

function renderStats() {
  $("stats").replaceChildren();
  $("filters").replaceChildren();
  for (const kind of ["all", ...categories]) {
    const count = kind === "all" ? pages.length : pages.filter((p) => p.kind === kind).length;
    const button = el("button", t(kind) + " " + count, "filter");
    button.type = "button";
    button.dataset.kind = kind;
    button.setAttribute("aria-pressed", String(kind === category));
    button.addEventListener("click", () => { category = kind; renderStats(); renderList(); });
    $("filters").append(button);
    if (kind !== "all") {
      const stat = el("div", undefined, "stat " + kind);
      stat.append(el("span", t(kind)), el("strong", String(count)));
      $("stats").append(stat);
    }
  }
}

function renderList() {
  const query = $("search").value.trim().toLocaleLowerCase();
  const visible = pages.filter((p) => (category === "all" || p.kind === category) &&
    JSON.stringify(p).toLocaleLowerCase().includes(query));
  $("page-count").textContent = visible.length + " / " + pages.length;
  $("page-list").replaceChildren();
  if (!visible.some((page) => page.key === selectedKey)) selectedKey = visible[0]?.key;
  for (const page of visible) {
    const button = el("button", undefined, "page-item");
    button.type = "button";
    button.classList.toggle("selected", page.key === selectedKey);
    button.setAttribute("aria-pressed", String(page.key === selectedKey));
    button.append(el("strong", page.title), el("span", page.key, "page-key"), badge(page.kind));
    button.addEventListener("click", () => { selectedKey = page.key; renderList(); });
    $("page-list").append(button);
  }
  if (!visible.length) $("page-list").append(el("p", t("noMatches"), "empty"));
  renderDetail(visible.find((page) => page.key === selectedKey));
}

function reasonText(reason) {
  const parts = reason.split(":");
  const names = { source_changed: "sourceChanged", global_summary: "globalSummary",
    child_affected: "childAffected", dependency_changed: "dependencyChanged" };
  return t(names[parts[0]] || parts[0]) + (parts.length > 1 ? ": " + parts.slice(1).join(":") : "");
}

function renderChain(evidence) {
  const card = el("section", undefined, "chain-card");
  const heading = el("div", undefined, "chain-heading");
  heading.append(badge(evidence.side), badge(evidence.resolution), code(short(evidence.commit)));
  card.append(heading);
  const route = el("div", undefined, "chain-route");
  const steps = evidence.chain || [];
  if (steps.length) {
    route.append(el("span", t("chainFrom"), "route-label"), code(steps[0].dependent_file));
    for (const step of steps) {
      route.append(el("span", "↓ " + t(step.edge.type), "route-edge"), code(step.dependency_file));
    }
    route.append(el("span", t("chainTo"), "route-label end"));
  }
  card.append(route, el("p", `${steps.length} ${t("hops")} · ${t("confidence")}: ${evidence.confidence}`, "muted"));
  const details = el("details");
  details.append(el("summary", t("edgeDetails")));
  for (const step of steps) {
    const node = el("div", undefined, "edge-detail");
    node.append(el("strong", t(step.edge.type)));
    for (const symbol of [step.source, step.target]) {
      node.append(code(`${symbol.file_path}:${symbol.start_line ?? "?"}-${symbol.end_line ?? "?"} · ${symbol.name}`));
    }
    node.append(el("p", t("edgeId") + ": " + step.edge.id, "muted"),
      el("p", `${t("confidence")}: ${step.edge.confidence} · ${t(step.edge.is_inferred ? "inferredEdge" : "extractedEdge")}`, "muted"),
      el("pre", JSON.stringify(step.edge.evidence || {}, null, 2)));
    details.append(node);
  }
  details.append(el("p", t("graphFingerprint"), "meta-label"), code(evidence.graph_fingerprint));
  card.append(details);
  return card;
}

function renderDetail(page) {
  const panel = $("page-detail");
  panel.replaceChildren();
  if (!page) { panel.append(el("p", t("selectPage"), "empty")); return; }
  panel.append(badge(page.kind), el("h2", page.title), code(page.key),
    el("p", t(page.kind + "Help"), "detail-help"));
  panel.append(el("h3", t("sources")));
  const sources = el("div", undefined, "source-files");
  for (const path of page.sources) sources.append(code(path));
  if (!page.sources.length) sources.append(el("p", t("noSources"), "muted"));
  panel.append(sources);
  if (page.reasons?.length) {
    panel.append(el("h3", t("reasons")));
    const list = el("ul", undefined, "reason-list");
    page.reasons.forEach((reason) => list.append(el("li", reasonText(reason))));
    panel.append(list);
  }
  const evidence = page.dependency_evidence || [];
  panel.append(el("h3", `${t("chains")} (${evidence.length})`));
  if (evidence.length) {
    panel.append(el("p", t("chainHelp"), "muted"));
    evidence.forEach((item) => panel.append(renderChain(item)));
  } else panel.append(el("p", t("noChains"), "muted"));
}

async function loadSnapshot() {
  const ticket = ++revision;
  $("workspace").hidden = true;
  message(t("loading"));
  try {
    const identity = $("snapshot").value;
    const data = await get("/api/snapshots/" + encodeURIComponent(identity));
    if (ticket !== revision) return;
    history.replaceState(null, "", "#" + identity);
    const preview = data.preview;
    if (preview.plan_kind === "code_catalog") {
      location.replace((document.documentElement.dataset.wikiBase || "") + "/planning#" + identity);
      return;
    }
    pages = normalize(data);
    selectedKey = null;
    $("repository").textContent = data.repository;
    $("branch").textContent = preview.branch;
    $("baseline").textContent = short(preview.baseline);
    $("baseline").title = preview.baseline || t("initial");
    $("commit").textContent = short(preview.commit);
    $("commit").title = preview.commit;
    $("scope").textContent = t("scope") + ": " + (data.scope.join(", ") || t("fullScope"));
    renderGraphs(preview.graphs);
    renderStats();
    renderList();
    $("changes").replaceChildren();
    for (const file of preview.changed_files || []) {
      const row = el("div", undefined, "changed-file");
      row.append(code(file.status), code((file.old_path ? file.old_path + " → " : "") + file.path));
      $("changes").append(row);
    }
    $("change-count").textContent = (preview.changed_files || []).length;
    $("raw-json").href = (document.documentElement.dataset.wikiBase || "") + "/api/snapshots/" + identity;
    $("workspace").hidden = false;
    message(preview.catalog_required ? t("catalogRequired") : "");
  } catch {
    if (ticket === revision) message(t("error"), true);
  }
}

async function refresh() {
  const previous = $("snapshot").value || location.hash.slice(1);
  ++revision;
  $("workspace").hidden = true;
  $("refresh").disabled = true;
  $("snapshot").disabled = true;
  message(t("loading"));
  try {
    const snapshots = await get("/api/snapshots");
    $("snapshot").replaceChildren();
    for (const snapshot of snapshots) {
      const option = el("option", `${snapshot.branch} · ${short(snapshot.commit_sha)} · ${new Date(snapshot.created_at * 1000).toLocaleString("zh-CN")}${snapshot.is_current ? " · " + t("current") : ""}`);
      option.value = snapshot.id;
      $("snapshot").append(option);
    }
    $("snapshot").disabled = !snapshots.length;
    if (!snapshots.length) { message(t("empty")); return; }
    if (snapshots.some((s) => s.id === previous)) $("snapshot").value = previous;
    await loadSnapshot();
  } catch { message(t("error"), true); }
  finally { $("refresh").disabled = false; }
}

async function initialize() {
  try {
    copy = await get("/i18n.json");
    document.title = t("product");
    document.querySelectorAll("[data-i18n]").forEach((node) => {
      node.textContent = t(node.dataset.i18n);
    });
    $("search").placeholder = t("search");
    $("snapshot").addEventListener("change", loadSnapshot);
    $("search").addEventListener("input", renderList);
    $("refresh").addEventListener("click", refresh);
    await refresh();
  } catch { message("Preview could not be loaded. Refresh to retry.", true); }
}

initialize();
