// SPDX-License-Identifier: Apache-2.0
"use strict";
let copy = {}, data = null, view = "files", selected = null, ticket = 0;
const expanded = new Set();
const catalogBase = document.documentElement.dataset.wikiBase || "";
const $ = (id) => document.getElementById(id);
const t = (key) => copy[key] || key;
let excludedDirectories = [];
function normalizeDirectory(path) { return path.trim().replace(/\\/g, "/").replace(/^(\.\/)+/, "").replace(/\/+$/, ""); }
function excludedPath(path) {
  return typeof path === "string" && excludedDirectories.some((dir) => path.startsWith(dir + "/"));
}
function includedPage(page) {
  return !(page.source_hints || []).some(excludedPath) && !(page.symbol_refs || []).some((symbol) => excludedPath(symbol.file_path));
}
function directoryStorageKey() { return "wiki-preview:excluded-directories:" + data.repository; }
function applyDirectories(clear = false) {
  const directories = clear ? [] : [...new Set($("excluded-directories").value.split(/\r?\n/).map(normalizeDirectory).filter(Boolean))];
  if (directories.some((path) => path.startsWith("/") || path.includes(":") || path.split("/").some((part) => part === ".." || part === ".") || /[*?]/.test(path))) {
    $("directory-filter-status").textContent = t("directoryFilterInvalid");
    return;
  }
  excludedDirectories = directories;
  $("excluded-directories").value = directories.join("\n");
  let saved = true;
  try { localStorage.setItem(directoryStorageKey(), JSON.stringify(directories)); } catch { saved = false; }
  renderSummary(allPages().filter(includedPage));
  render();
  if (!saved) $("directory-filter-status").append(" · " + t("directoryFilterNotSaved"));
}
function el(tag, text, cls) {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (cls) node.className = cls;
  return node;
}
async function get(path) {
  const response = await fetch(catalogBase + path, {cache:"no-store"});
  if (!response.ok) throw new Error("Unreadable plan");
  return response.json();
}
function flatten(items) { return items.flatMap((item) => [item, ...flatten(item.children || [])]); }
function allPages() { return flatten(data.preview.code_plan.file_catalog).concat(data.preview.code_plan.feature_catalog); }
function catalogStateKey() { return "wiki-plan-reader:" + data.repository + ":" + data.snapshot_id; }
function saveCatalogState() {
  if (!data) return;
  try { localStorage.setItem(catalogStateKey(), JSON.stringify({view, selected, query: $("search").value, expanded: [...expanded]})); } catch {}
}
function restoreCatalogState() {
  let state = {};
  try { state = JSON.parse(localStorage.getItem(catalogStateKey()) || "{}"); } catch {}
  view = state?.view === "features" ? "features" : "files";
  selected = typeof state?.selected === "string" ? state.selected : null;
  $("search").value = typeof state?.query === "string" ? state.query : "";
  expanded.clear();
  if (Array.isArray(state?.expanded)) for (const slug of state.expanded) if (typeof slug === "string") expanded.add(slug);
}
function openPage(slug) {
  const page = allPages().find((item) => item.slug === slug);
  if (!page) return;
  view = page.page_type === "feature" ? "features" : "files";
  selected = slug;
  $("search").value = "";
  render();
}
function render() {
  if (!data) return;
  const catalogPages = allPages().filter((page) => page.page_type !== "directory");
  const filteredPages = catalogPages.filter(includedPage);
  $("directory-filter-status").textContent = `${t("directoryFilterHidden")}: ${catalogPages.length - filteredPages.length} · ${t("directoryFilterRules")}: ${excludedDirectories.length}`;
  $("views").replaceChildren();
  for (const kind of ["files", "features"]) {
    const button = el("button", t(kind), "filter");
    button.setAttribute("aria-pressed", String(view === kind));
    button.onclick = () => { view = kind; selected = null; render(); };
    $("views").append(button);
  }
  const query = $("search").value.toLocaleLowerCase();
  const roots = view === "files" ? data.preview.code_plan.file_catalog : data.preview.code_plan.feature_catalog;
  const pages = flatten(roots).filter((page) => page.page_type !== "directory" && includedPage(page) &&
    JSON.stringify(page).toLocaleLowerCase().includes(query));
  if (!pages.some((page) => page.slug === selected)) selected = pages[0]?.slug;
  $("page-list").replaceChildren();
  const visible = new Set(pages.map((page) => page.slug));
  function tree(items, depth) {
    for (const page of items) {
      if (page.page_type === "directory") {
        if (!flatten(page.children).some((child) => visible.has(child.slug))) continue;
        const group = el("details", undefined, "catalog-directory");
        group.open = expanded.has(page.slug) || flatten(page.children).some((child) => child.slug === selected) || Boolean(query);
        group.append(el("summary", page.title));
        const children = el("div");
        const fill = () => {
          const parent = currentList;
          currentList = children;
          children.replaceChildren();
          tree(page.children, depth + 1);
          currentList = parent;
        };
        if (group.open) fill();
        group.ontoggle = () => { if (group.open) { expanded.add(page.slug); fill(); } else { expanded.delete(page.slug); children.replaceChildren(); } saveCatalogState(); };
        group.append(children);
        currentList.append(group);
      } else {
        if (!visible.has(page.slug) && !flatten(page.children || []).some((child) => visible.has(child.slug))) continue;
        if (visible.has(page.slug)) {
          const button = el("button", undefined, "page-item");
          button.classList.toggle("selected", selected === page.slug);
          button.setAttribute("aria-pressed", String(selected === page.slug));
          button.append(el("strong", page.title), el("span", t(page.page_type), "page-key"));
          button.onclick = () => { selected = page.slug; render(); };
          currentList.append(button);
        }
        if (page.children?.length) {
          const group = el("details", undefined, "catalog-directory");
          group.open = expanded.has(page.slug) || page.children.some((child) => child.slug === selected) || Boolean(query);
          group.append(el("summary", `${t("responsibilityPages")} (${page.children.length})`));
          const children = el("div");
          const fill = () => { const parent = currentList; currentList = children; children.replaceChildren(); tree(page.children,depth+1); currentList = parent; };
          if (group.open) fill();
          group.ontoggle = () => { if (group.open) { expanded.add(page.slug); fill(); } else { expanded.delete(page.slug); children.replaceChildren(); } saveCatalogState(); };
          group.append(children);
          currentList.append(group);
        }
      }
    }
  }
  let currentList = $("page-list");
  tree(roots, 0);
  if (!pages.length) currentList.append(el("p", t("noMatches"), "empty"));
  detail(pages.find((page) => page.slug === selected));
  saveCatalogState();
}
function links(panel, slugs, title) {
  if (!slugs?.length) return;
  panel.append(el("h3", t(title)));
  for (const slug of slugs) {
    const target = allPages().find((page) => page.slug === slug);
    if (!target || !includedPage(target)) continue;
    const button = el("button", target.title, "catalog-link");
    button.onclick = () => openPage(slug);
    panel.append(button);
  }
}
function detail(page) {
  const panel = $("page-detail");
  panel.replaceChildren();
  if (!page) { panel.append(el("p", t("selectPage"))); return; }
  panel.append(el("span", t(page.page_type), "badge"), el("h2", page.title), el("p", t("proposalNotice"), "detail-help"));
  if (page.line_count !== undefined) panel.append(el("p", t("lineCount") + ": " + page.line_count));
  if (page.grouping_basis) panel.append(el("p", t(page.grouping_basis), "muted"));
  if (page.resolution) panel.append(el("p", t(page.resolution), "badge inferred"));
  const symbols = page.symbol_refs || [];
  panel.append(el("h3", `${t("plannedSymbols")} (${symbols.length})`));
  const box = el("div", undefined, "source-files");
  for (const symbol of symbols) box.append(el("code", `${symbol.name} · ${symbol.file_path}:${symbol.start_line ?? "?"}-${symbol.end_line ?? "?"}`));
  panel.append(box);
  if (page.coverage) panel.append(el("p", `${t("symbolCoverage")}: ${page.coverage.indexed_symbols} / ${page.coverage.assigned_symbols}`, "muted"));
  if (page.gaps?.length) {
    panel.append(el("h3", t("gaps")));
    for (const gap of page.gaps) panel.append(el("p", t(gap), "muted"));
  }
  links(panel, page.children?.map((child) => child.slug), "responsibilityPages");
  links(panel, page.file_page_links, "relatedFiles");
  links(panel, page.feature_links, "relatedFeatures");
  if (page.dependency_evidence?.length) {
    const details = el("details");
    details.append(el("summary", `${t("chains")} (${page.dependency_evidence.length})`));
    const nodes = new Map(symbols.map((symbol) => [symbol.id, symbol]));
    for (const edge of page.dependency_evidence) {
      const source = nodes.get(edge.source_id), target = nodes.get(edge.target_id);
      details.append(el("p", `${source?.name} → ${t(edge.type)} → ${target?.name} · ${edge.confidence}`, "reason-list"),
        el("code", `${source?.file_path} → ${target?.file_path}`));
    }
    panel.append(details);
  }
}
async function load() {
  const version = ++ticket;
  $("workspace").hidden = true;
  $("message").hidden = false;
  $("message").textContent = t("loading");
  try {
    const result = await get("/api/snapshots/" + $("snapshot").value);
    if (version !== ticket) return;
    if (result.preview.plan_kind !== "code_catalog") throw new Error("Not a code catalog plan");
    data = result;
    restoreCatalogState();
    history.replaceState(null, "", "#" + result.snapshot_id);
    if (window.parent !== window) window.parent.postMessage({type: "wiki-location", repository: result.repository, commit: result.preview.commit, scope: result.scope}, location.origin);
    $("repository").textContent = result.repository;
    $("branch").textContent = result.preview.branch;
    $("commit").textContent = result.preview.commit;
    $("producer").textContent = "CodeWiki " + result.preview.producer.codewiki_commit;
    const policy = result.preview.generation_policy;
    if (policy) $("producer").append(el("p", `${t("generationPolicy")}: AI Mana / ${policy.model} · ${t("generationNotInvoked")}`));
    if (result.preview.analysis_gaps) $("producer").append(el("p", `${t("parseFailures")}: ${result.preview.analysis_gaps.length} · ${t("unindexedPlanFiles")}: ${result.preview.unindexed_files.length}`));
    try {
      const stored = JSON.parse(localStorage.getItem(directoryStorageKey()) || "[]");
      excludedDirectories = Array.isArray(stored) ? stored.filter((path) => typeof path === "string" && path.length) : [];
    } catch { excludedDirectories = []; }
    $("excluded-directories").value = excludedDirectories.join("\n");
    $("raw-json").href = catalogBase + "/api/snapshots/" + result.snapshot_id;
    $("workspace").hidden = false;
    $("message").hidden = true;
    renderSummary(allPages().filter(includedPage));
    render();
  } catch { $("message").textContent = t("error"); }
}
function renderSummary(pages) {
    const plan = data.preview.code_plan;
    $("stats").replaceChildren();
    for (const [key, count] of [["files", pages.filter((page) => page.page_type === "file").length], ["features", pages.filter((page) => page.page_type === "feature").length],
      ["responsibilityPages", pages.filter((page) => page.page_type === "responsibility").length], ["deferredFeatures", (plan.deferred_feature_entries || []).filter((symbol) => !excludedPath(symbol.file_path)).length],
      ["excludedFiles", (plan.excluded_files || []).filter((path) => !excludedPath(path)).length]]) {
      const stat = el("div", undefined, "stat");
      stat.append(el("span", t(key)), el("strong", String(count)));
      $("stats").append(stat);
    }
    const deferred = el("details");
    deferred.append(el("summary", t("deferredFeatures") + " / " + t("excludedFiles")));
    for (const symbol of plan.deferred_feature_entries || []) if (!excludedPath(symbol.file_path)) deferred.append(el("code", `${symbol.name} · ${symbol.file_path}`), el("br"));
    for (const path of plan.excluded_files || []) if (!excludedPath(path)) deferred.append(el("code", path), el("br"));
    $("producer").querySelector("details")?.remove();
    $("producer").append(deferred);
}
async function initialize() {
  copy = await get("/i18n.json");
  for (const [id, key] of Object.entries({product:"planProduct", readonly:"readonly", heading:"planHeading", subtitle:"planSubtitle", offline:"offline", "snapshot-label":"snapshot", refresh:"refresh", notice:"proposalNotice", footer:"planFooter", "raw-json":"rawJson", "search-label":"search", "back-review":"backReview"})) $(id).textContent = t(key);
  $("search").placeholder = t("search");
  for (const [id, key] of Object.entries({"directory-filter-title":"directoryFilterTitle", "directory-filter-label":"directoryFilterLabel", "directory-filter-help":"directoryFilterHelp", "apply-directories":"directoryFilterApply", "clear-directories":"directoryFilterClear"})) $(id).textContent = t(key);
  $("apply-directories").onclick = () => applyDirectories();
  $("clear-directories").onclick = () => applyDirectories(true);
  $("search").oninput = render;
  $("snapshot").onchange = load;
  $("refresh").onclick = refresh;
  await refresh();
}
async function refresh() {
  const previous = $("snapshot").value || location.hash.slice(1);
  try {
    const snapshots = await get("/api/snapshots");
    $("snapshot").replaceChildren();
    for (const snapshot of snapshots) {
      const option = el("option", `${snapshot.branch} · ${snapshot.commit_sha.slice(0,12)} · ${new Date(snapshot.created_at*1000).toLocaleString("zh-CN")} · ${snapshot.id.slice(0,6)}`);
      option.value = snapshot.id;
      $("snapshot").append(option);
    }
    if (!snapshots.length) { $("message").textContent = t("empty"); $("workspace").hidden = true; return; }
    if (snapshots.some((snapshot) => snapshot.id === previous)) $("snapshot").value = previous;
    await load();
  } catch { $("message").textContent = t("error"); }
}
initialize().catch(() => { $("message").textContent = "Plan could not be loaded."; });
