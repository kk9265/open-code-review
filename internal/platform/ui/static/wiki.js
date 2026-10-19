// SPDX-License-Identifier: Apache-2.0
"use strict";
const frame = document.getElementById("wiki-frame");
const view = document.querySelector(".wiki-workspace").dataset.view;
const prefix = "/wiki/" + view + "/";
const key = "review-wiki-location:" + view;
function remember() {
  try {
    const loc = frame.contentWindow.location;
    if (loc.origin === location.origin && loc.pathname.startsWith(prefix)) {
      localStorage.setItem(key, loc.pathname + loc.search + loc.hash);
    }
  } catch {}
}
try {
  const previous = localStorage.getItem(key);
  if (previous && new URL(previous, location.origin).origin === location.origin && new URL(previous, location.origin).pathname.startsWith(prefix)) frame.src = previous;
} catch {}
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
if (view === "read") fetch("/wiki/read/api/catalog").then((response) => {
  if (!response.ok) throw new Error("Unavailable Wiki metadata");
  return response.json();
}).then((data) => {
  document.getElementById("wiki-version").textContent = data.repository + " / " + data.scope + " · " + data.commit.slice(0, 12);
}).catch(() => { document.getElementById("wiki-version").textContent = ""; });
