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

highlightSource();
window.addEventListener("hashchange", highlightSource);
function highlightFunction() {
  for (const link of document.querySelectorAll(".reader-function")) {
    const url = new URL(link.href);
    if (url.pathname === location.pathname && url.hash === location.hash && url.hash) {
      link.setAttribute("aria-current", "location");
    } else {
      link.removeAttribute("aria-current");
    }
  }
}
highlightFunction();
window.addEventListener("hashchange", highlightFunction);

function setupReaderNavigation() {
  const layout = document.querySelector(".reader-layout");
  if (!layout) return;
  const key = "wiki-reader:" + layout.dataset.repository + ":" + (layout.dataset.scope || "") + ":" + layout.dataset.commit + ":" + (layout.dataset.mode || "");
  let state = {view: "files", query: "", expanded: []};
  try { state = {...state, ...JSON.parse(localStorage.getItem(key) || "{}")}; } catch {}
  const views = [...layout.querySelectorAll("[data-reader-view]")].map((button) => button.dataset.readerView);
  if (!views.includes(state.view)) state.view = "files";
  if (typeof state.query !== "string") state.query = "";
  if (!Array.isArray(state.expanded)) state.expanded = [];
  if (state.page !== readerPath) {
    if (document.querySelector('#reader-features [aria-current="page"]')) state.view = "features";
    if (document.querySelector('#reader-files [aria-current="page"]')) state.view = "files";
    if (document.querySelector('#reader-functions [aria-current="page"]')) state.view = "functions";
  }
  state.page = readerPath;
  const search = document.getElementById("reader-search");
  search.value = state.query;
  function save() { try { localStorage.setItem(key, JSON.stringify(state)); } catch {} }
  function render() {
    const query = search.value.toLocaleLowerCase().trim();
    state.query = search.value;
    let matches = 0;
    for (const view of views) {
      const panel = document.getElementById("reader-" + view);
      panel.hidden = state.view !== view;
      for (const link of panel.querySelectorAll(".reader-page")) {
        link.hidden = !link.dataset.search.toLocaleLowerCase().includes(query);
        if (!link.hidden && state.view === view) matches++;
      }
      for (const group of panel.querySelectorAll(".reader-file")) {
        const parent = group.querySelector("summary .reader-page");
        if (parent?.dataset.search.toLocaleLowerCase().includes(query)) {
          group.querySelectorAll(".reader-page").forEach((link) => { link.hidden = false; });
        }
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
