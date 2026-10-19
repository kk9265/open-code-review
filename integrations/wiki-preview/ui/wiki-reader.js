// SPDX-License-Identifier: Apache-2.0
"use strict";
const readerBase = document.documentElement.dataset.wikiBase || "";
const readerPath = location.pathname.slice(readerBase.length);

function highlightSource() {
  if (!readerPath.startsWith("/source/")) return;
  document.querySelectorAll(".source-selected").forEach((line) => line.classList.remove("source-selected"));
  const range = /^#L(\d+)(?:-L(\d+))?$/.exec(location.hash);
  if (!range) return;
  const start = Number(range[1]), end = Number(range[2] || range[1]);
  const first = document.getElementById("L" + start);
  if (!first || end < start) return;
  for (let line = start; line <= Math.min(end, start + 1000); line++) {
    document.getElementById("L" + line)?.classList.add("source-selected");
  }
  first.scrollIntoView({block: "center"});
}

async function enhanceReferences() {
  if (!readerPath.startsWith("/page/")) return;
  const response = await fetch(readerBase + "/i18n.json");
  if (!response.ok) return;
  const copy = await response.json();
  const main = document.querySelector("main");
  const sourceHeading = [...main.querySelectorAll("h2")].find((node) => node.textContent.trim() === "Sources");
  let evidence;
  if (sourceHeading) {
    evidence = document.createElement("details");
    evidence.className = "source-evidence";
    const summary = document.createElement("summary");
    evidence.append(summary);
    let node = sourceHeading.nextElementSibling;
    sourceHeading.replaceWith(evidence);
    while (node && !/^H[12]$/.test(node.tagName)) {
      const next = node.nextElementSibling;
      evidence.append(node);
      node = next;
    }
    const count = new Set(evidence.textContent.match(/\bS\d+\b/g) || []).size;
    summary.textContent = copy.sourceEvidenceTitle.replace("{count}", String(count));
  }
  for (const link of main.querySelectorAll('a[href^="' + readerBase + '/source/"]')) {
    if (evidence?.contains(link)) continue;
    const label = link.textContent.trim();
    if (!/^S\d+$/.test(label)) continue;
    const url = new URL(link.href);
    const range = /^#L(\d+)(?:-L(\d+))?$/.exec(url.hash);
    if (!range) continue;
    const file = decodeURIComponent(url.pathname.slice((readerBase + "/source/").length));
    const lines = range[2] && range[2] !== range[1] ? range[1] + "–" + range[2] : range[1];
    const description = copy.sourceReferenceLabel.replace("{file}", file).replace("{lines}", lines);
    link.dataset.citation = label;
    link.title = description;
    link.setAttribute("aria-label", description);
    link.textContent = "[" + label.slice(1) + "]";
    const marker = document.createElement("sup");
    marker.className = "source-reference";
    link.replaceWith(marker);
    marker.append(link);
  }
}

highlightSource();
window.addEventListener("hashchange", highlightSource);
enhanceReferences().catch(() => {});

function setupReaderNavigation() {
  const layout = document.querySelector(".reader-layout");
  if (!layout) return;
  const key = "wiki-reader:" + layout.dataset.repository + ":" + layout.dataset.commit;
  let state = {view: "files", query: "", expanded: []};
  try { state = {...state, ...JSON.parse(localStorage.getItem(key) || "{}")}; } catch {}
  if (!["files", "features"].includes(state.view)) state.view = "files";
  if (typeof state.query !== "string") state.query = "";
  if (!Array.isArray(state.expanded)) state.expanded = [];
  if (state.page !== readerPath) {
    if (document.querySelector('#reader-features [aria-current="page"]')) state.view = "features";
    if (document.querySelector('#reader-files [aria-current="page"]')) state.view = "files";
  }
  state.page = readerPath;
  const search = document.getElementById("reader-search");
  search.value = state.query;
  function save() { try { localStorage.setItem(key, JSON.stringify(state)); } catch {} }
  function render() {
    const query = search.value.toLocaleLowerCase().trim();
    state.query = search.value;
    let matches = 0;
    for (const view of ["files", "features"]) {
      const panel = document.getElementById("reader-" + view);
      panel.hidden = state.view !== view;
      for (const link of panel.querySelectorAll(".reader-page")) {
        link.hidden = !link.dataset.search.toLocaleLowerCase().includes(query);
        if (!link.hidden && state.view === view) matches++;
      }
      for (const group of panel.querySelectorAll(".reader-file")) {
        group.hidden = ![...group.querySelectorAll(".reader-page")].some((link) => !link.hidden);
        if (!group.hidden) group.querySelector("summary .reader-page")?.removeAttribute("hidden");
        if (group.tagName === "DETAILS" && query && !group.hidden) group.open = true;
      }
    }
    for (const button of layout.querySelectorAll("[data-reader-view]")) button.setAttribute("aria-pressed", String(button.dataset.readerView === state.view));
    document.getElementById("reader-empty").hidden = matches > 0;
    save();
  }
  for (const group of layout.querySelectorAll("details.reader-file")) {
    if (state.expanded.includes(group.dataset.slug)) group.open = true;
    group.addEventListener("toggle", () => {
      state.expanded = [...layout.querySelectorAll("details.reader-file[open]")].map((node) => node.dataset.slug);
      save();
    });
  }
  for (const button of layout.querySelectorAll("[data-reader-view]")) button.onclick = () => { state.view = button.dataset.readerView; render(); };
  search.oninput = render;
  render();
}
setupReaderNavigation();
