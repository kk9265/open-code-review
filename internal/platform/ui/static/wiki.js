// SPDX-License-Identifier: Apache-2.0
"use strict";
const frame = document.getElementById("wiki-frame");
const view = document.querySelector(".wiki-workspace").dataset.view;
const workspace = document.querySelector(".wiki-workspace");
const prefix = workspace.dataset.prefix + "/" + view + "/";
const key = "review-wiki-location:" + workspace.dataset.scope + ":" + view;
function remember() {
  try {
    const loc = frame.contentWindow.location;
    if (loc.origin === location.origin && loc.pathname.startsWith(prefix)) {
      localStorage.setItem(key, loc.pathname + loc.search + loc.hash);
    }
  } catch {}
}
function restore(pages) {
  try {
    const previous = localStorage.getItem(key);
    if (!previous) return;
    const url = new URL(previous, location.origin);
    if (url.origin !== location.origin || !url.pathname.startsWith(prefix)) return;
    if (Array.isArray(pages) && url.pathname !== prefix) {
      const slug = url.pathname.startsWith(prefix + "page/") ? url.pathname.slice((prefix + "page/").length) : url.searchParams.get("page");
      if (!pages.includes(slug)) { localStorage.removeItem(key); frame.src = prefix; return; }
    }
    frame.src = previous;
  } catch {}
}
if (view !== "read") restore();
frame.addEventListener("load", () => {
  remember();
  try { frame.contentWindow.addEventListener("hashchange", remember); } catch {}
});
window.addEventListener("message", (event) => {
  if (event.source === frame.contentWindow && event.origin === location.origin && event.data?.type === "wiki-location") {
    remember();
    if (typeof event.data.repository === "string" && /^[a-f0-9]{40}$/.test(event.data.commit)) {
      const repository = event.data.repository.split(/[\\/]/).pop();
      document.getElementById("wiki-version").textContent = repository + " · " + event.data.commit.slice(0, 12);
    }
  }
});
if (view === "read") fetch(prefix + "api/catalog").then((response) => {
  if (!response.ok) throw new Error("Unavailable Wiki metadata");
  return response.json();
}).then((data) => {
  document.getElementById("wiki-version").textContent = data.repository + " / " + data.scope + " · " + data.commit.slice(0, 12);
  restore(data.pages);
}).catch(() => { document.getElementById("wiki-version").textContent = ""; });
