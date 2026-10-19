// SPDX-License-Identifier: Apache-2.0
"use strict";
const diagramBase = document.documentElement.dataset.wikiBase || "";

async function renderDiagrams() {
  const blocks = [...document.querySelectorAll("pre > code.language-mermaid")];
  if (!blocks.length) return;
  const copy = await fetch(diagramBase + "/i18n.json").then((response) => response.json());
  let mermaid;
  try {
    ({default: mermaid} = await import(diagramBase + "/vendor/mermaid/mermaid.esm.min.mjs"));
    mermaid.initialize({startOnLoad: false, securityLevel: "strict", maxTextSize: 100000,
      flowchart: {useMaxWidth: false, htmlLabels: false}});
  } catch {
    for (const code of blocks) code.parentElement.before(message(copy.diagramUnavailable));
    return;
  }
  for (const [index, code] of blocks.entries()) {
    try {
      const {svg} = await mermaid.render("wiki-diagram-" + index, code.textContent);
      const canvas = document.createElement("div");
      canvas.className = "wiki-diagram";
      canvas.setAttribute("role", "img");
      canvas.setAttribute("aria-label", copy.diagramLabel);
      canvas.innerHTML = svg;
      const details = document.createElement("details");
      const summary = document.createElement("summary");
      summary.textContent = copy.diagramSource;
      details.append(summary);
      const pre = code.parentElement;
      const toolbar = document.createElement("div");
      toolbar.className = "diagram-toolbar";
      pre.replaceWith(toolbar, canvas, details);
      details.append(pre);
      const diagram = canvas.querySelector("svg");
      const width = diagram.viewBox.baseVal.width;
      let scale = 1;
      const resize = (value) => { scale = value; diagram.style.width = width * scale + "px"; };
      const fit = () => resize(Math.min(1, (canvas.clientWidth - 32) / width));
      for (const [label, action] of [[copy.diagramFit, fit],
        [copy.diagramZoomIn, () => resize(Math.min(4, scale * 1.5))],
        [copy.diagramZoomOut, () => resize(Math.max(0.05, scale / 1.5))]]) {
        const button = document.createElement("button");
        button.type = "button";
        button.textContent = label;
        button.onclick = action;
        toolbar.append(button);
      }
      fit();
    } catch {
      document.getElementById("dwiki-diagram-" + index)?.remove();
      code.parentElement.before(message(copy.diagramInvalid));
    }
  }
}

function message(text) {
  const node = document.createElement("p");
  node.className = "muted";
  node.textContent = text;
  return node;
}

renderDiagrams().catch(() => {});
