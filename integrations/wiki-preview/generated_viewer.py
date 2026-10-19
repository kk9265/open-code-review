# SPDX-License-Identifier: Apache-2.0
import argparse
import hashlib
import json
import subprocess
from html import escape
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, quote, unquote, urlsplit

from markdown_it import MarkdownIt

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1] / ".ocr-data/wiki-loginserver-generated"
LOCALE = HERE / "ui/i18n/zh-CN.json"
COPY = json.loads(LOCALE.read_text(encoding="utf-8"))
STYLE = """body{margin:0;font:15px/1.8 "Segoe UI","Microsoft YaHei",sans-serif;color:#213431;background:#f4f7f6}header{padding:14px 28px;background:white;border-bottom:1px solid #dce5e1;display:flex;gap:24px;align-items:center}header strong{margin-right:auto}a{color:#187561}main{max-width:1150px;margin:25px auto;padding:25px;background:white;border:1px solid #dce5e1;border-radius:10px}pre{overflow:auto;background:#f4f7f6;padding:16px}code{font-family:Consolas,monospace}table{border-collapse:collapse}td,th{border:1px solid #dce5e1;padding:8px}li{margin:8px 0}.muted{color:#71837c}.catalog{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:20px}h1{font-size:26px}h2{font-size:21px}img{max-width:100%}"""
MARKDOWN = MarkdownIt("commonmark", {"html": False}).enable("table")
original_link_open = MARKDOWN.renderer.renderToken


def local_source_link(tokens, index, options, env):
    token = tokens[index]
    href = token.attrGet("href") or ""
    marker = "/blob/" + env["commit"] + "/"
    if marker in href:
        path = href.split(marker, 1)[1]
        token.attrSet(
            "href",
            "/source/"
            + quote(unquote(path.split("#", 1)[0]), safe="/")
            + ("?page=" + quote(env["page_slug"]) if env.get("page_slug") else "")
            + ("#" + path.split("#", 1)[1] if "#" in path else ""),
        )
    return original_link_open(tokens, index, options, env)


MARKDOWN.renderer.rules["link_open"] = local_source_link
STYLE += ".wiki-diagram{max-height:75vh;overflow:auto;padding:16px;border:1px solid #dce5e1;border-radius:8px;margin:16px 0;background:white}.wiki-diagram svg{max-width:none;height:auto}details{margin:10px 0}summary{cursor:pointer;color:#187561}"
MERMAID = ROOT.parent / "wiki-mermaid/node_modules/mermaid/dist"


def t(key):
    return escape(COPY[key])


def reader_layout(pages, current, content, manifest):
    pages = sorted(pages, key=lambda page: page["title"].casefold())
    selected = next(
        (page for page in pages if "/page/" + page["slug"] == current), None
    )

    def page_link(page):
        active = ' aria-current="page"' if page == selected else ""
        search = (
            page["title"]
            + " "
            + " ".join(ref["file_path"] for ref in page["source_refs"])
        )
        return (
            '<a class="reader-page" data-search="'
            + escape(search, quote=True)
            + '" href="/page/'
            + escape(page["slug"], quote=True)
            + '"'
            + active
            + ">"
            + escape(page["title"])
            + "</a>"
        )

    navigation = (
        '<aside class="reader-sidebar"><p class="muted">'
        + escape(manifest["repository"])
        + " · "
        + escape(manifest["commit"][:12])
        + '</p><label for="reader-search">'
        + t("readerSearch")
        + '</label><input id="reader-search" type="search"><div class="reader-tabs"><button type="button" data-reader-view="files">'
        + t("files")
        + '</button><button type="button" data-reader-view="features">'
        + t("features")
        + '</button></div><nav id="reader-files" aria-label="'
        + t("files")
        + '">'
    )
    for page in pages:
        if page["page_type"] != "file":
            continue
        children = [
            child for child in pages if child.get("parent_slug") == page["slug"]
        ]
        if children:
            opened = (
                " open"
                if selected and selected.get("parent_slug") == page["slug"]
                else ""
            )
            navigation += (
                '<details class="reader-file" data-slug="'
                + page["slug"]
                + '"'
                + opened
                + "><summary>"
                + page_link(page)
                + '</summary><div class="reader-children">'
                + "".join(page_link(child) for child in children)
                + "</div></details>"
            )
        else:
            navigation += '<div class="reader-file">' + page_link(page) + "</div>"
    navigation += (
        '</nav><nav id="reader-features" aria-label="'
        + t("features")
        + '" hidden>'
        + "".join(page_link(page) for page in pages if page["page_type"] == "feature")
        + '</nav><p id="reader-empty" hidden>'
        + t("noMatches")
        + "</p></aside>"
    )
    crumbs = '<a href="/">' + t("generatedCatalog") + "</a>"
    if selected:
        parent = next(
            (page for page in pages if page["slug"] == selected.get("parent_slug")),
            None,
        )
        crumbs += " / " + t(
            "features" if selected["page_type"] == "feature" else "files"
        )
        if parent:
            crumbs += (
                ' / <a href="/page/'
                + parent["slug"]
                + '">'
                + escape(parent["title"])
                + "</a>"
            )
        crumbs += " / <span>" + escape(selected["title"]) + "</span>"
    return (
        '<div class="reader-layout" data-repository="'
        + escape(manifest["repository"], quote=True)
        + '" data-commit="'
        + manifest["commit"]
        + '">'
        + navigation
        + '<article class="reader-article"><nav class="reader-breadcrumbs" aria-label="'
        + t("readerBreadcrumbs")
        + '">'
        + crumbs
        + "</nav>"
        + content
        + "</article></div>"
    )


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        path = urlsplit(self.path).path
        if path == "/style.css":
            body, kind, status = STYLE, "text/css; charset=utf-8", 200
        elif path == "/api/catalog":
            manifest = json.loads((ROOT / "manifest.json").read_text(encoding="utf-8"))
            body = json.dumps(
                {key: manifest[key] for key in ("repository", "scope", "commit")}
            )
            kind, status = "application/json; charset=utf-8", 200
        elif path in (
            "/wiki-diagrams.js",
            "/wiki-reader.js",
            "/wiki-reader.css",
            "/i18n.json",
        ):
            asset = (
                LOCALE
                if path == "/i18n.json"
                else LOCALE.parents[1] / path.removeprefix("/")
            )
            body = asset.read_text(encoding="utf-8")
            kind, status = (
                (
                    "application/json"
                    if path == "/i18n.json"
                    else "text/css"
                    if path.endswith(".css")
                    else "text/javascript"
                )
                + "; charset=utf-8",
                200,
            )
        elif path.startswith("/vendor/mermaid/"):
            asset = (MERMAID / unquote(path.removeprefix("/vendor/mermaid/"))).resolve()
            if (
                asset.is_relative_to(MERMAID.resolve())
                and asset.is_file()
                and asset.suffix in (".mjs", ".js")
            ):
                body, kind, status = (
                    asset.read_text(encoding="utf-8"),
                    "text/javascript; charset=utf-8",
                    200,
                )
            else:
                body, kind, status = "Not found.", "text/plain; charset=utf-8", 404
        else:
            try:
                progress = json.loads(
                    (ROOT / "progress.json").read_text(encoding="utf-8")
                )
                manifest = json.loads(
                    (ROOT / "manifest.json").read_text(encoding="utf-8")
                )
                pages = manifest["pages"]
                if path == "/":
                    body = (
                        '<h1>LoginServer Wiki</h1><p class="muted">AI Mana / '
                        + escape(progress["model"])
                        + " · "
                        + t("generatedOnlyByTool")
                        + "</p>"
                    )
                    body += (
                        '<p class="muted">'
                        + escape(
                            " · ".join(
                                model + ": " + str(count)
                                for model, count in progress.get(
                                    "generated_models", {}
                                ).items()
                            )
                        )
                        + "</p>"
                    )
                    body += f"<p>{t('generatedProgress')}: {progress['completed_pages']} / {progress['planned_pages']}</p>"
                    body += "<p>" + t("readerChoosePage") + "</p>"
                    status = 200
                elif path.startswith("/source/"):
                    source_path = unquote(path.removeprefix("/source/"))
                    allowed = {
                        ref["file_path"]
                        for page in pages
                        for ref in page["source_refs"]
                    }
                    if source_path not in allowed:
                        body, status = "<p>Source not found.</p>", 404
                    else:
                        source = subprocess.check_output(
                            [
                                "git",
                                "-C",
                                str(ROOT.parent / "wiki-zgame-plan/zgame"),
                                "show",
                                manifest["commit"] + ":" + source_path,
                            ]
                        ).decode("utf-8", errors="replace")
                        from_page = parse_qs(urlsplit(self.path).query).get(
                            "page", [""]
                        )[0]
                        back = (
                            "/page/" + from_page
                            if any(page["slug"] == from_page for page in pages)
                            else "/"
                        )
                        body = (
                            '<p><a href="'
                            + escape(back, quote=True)
                            + '">'
                            + t("readerBack")
                            + "</a></p><h1>"
                            + escape(source_path)
                            + "</h1><p><code>"
                            + escape(manifest["commit"])
                            + "</code></p><pre>"
                        )
                        body += (
                            "\n".join(
                                '<span id="L'
                                + str(index)
                                + '">'
                                + str(index)
                                + "  "
                                + escape(line)
                                + "</span>"
                                for index, line in enumerate(source.splitlines(), 1)
                            )
                            + "</pre>"
                        )
                        status = 200
                else:
                    page = next(
                        (item for item in pages if "/page/" + item["slug"] == path),
                        None,
                    )
                    if page is None:
                        body, status = "<p>Page not found.</p>", 404
                    else:
                        markdown = page["markdown"]
                        assert (
                            hashlib.sha256(markdown.encode()).hexdigest()
                            == page["markdown_sha256"]
                        )
                        assert (
                            ROOT / "markdown" / (page["slug"] + ".md")
                        ).read_bytes() == markdown.encode()
                        body = (
                            '<p class="muted">AI Mana / '
                            + escape(page.get("generation_model", "glm-5.3"))
                            + "</p>"
                            + MARKDOWN.render(
                                markdown,
                                {
                                    "commit": manifest["commit"],
                                    "page_slug": page["slug"],
                                },
                            )
                        )
                        status = 200
                if path == "/" or path.startswith("/page/"):
                    body = reader_layout(pages, path, body, manifest)
                body = (
                    '<!DOCTYPE html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>LoginServer Wiki</title><link rel="stylesheet" href="/style.css"><link rel="stylesheet" href="/wiki-reader.css"><script type="module" src="/wiki-reader.js"></script><script type="module" src="/wiki-diagrams.js"></script><header><strong>LoginServer Wiki</strong><a href="http://127.0.0.1:5490/planning">'
                    + t("generatedPlanning")
                    + '</a><a href="http://127.0.0.1:5484/reviews">'
                    + t("backReview")
                    + "</a></header><main>"
                    + body
                    + "</main></html>"
                )
                kind = "text/html; charset=utf-8"
            except (OSError, ValueError, KeyError, AssertionError):
                body, kind, status = (
                    "Wiki output is not ready or failed integrity verification.",
                    "text/plain; charset=utf-8",
                    503,
                )
        encoded = body.encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(encoded)))
        self.send_header("Cache-Control", "no-store")
        self.send_header(
            "Content-Security-Policy",
            "default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'",
        )
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(encoded)


def main():
    global ROOT, MERMAID
    parser = argparse.ArgumentParser(description="Read-only generated Wiki reader")
    parser.add_argument("--state", type=Path, default=ROOT)
    parser.add_argument("--port", type=int, default=5491)
    args = parser.parse_args()
    ROOT = args.state.resolve()
    MERMAID = ROOT.parent / "wiki-mermaid/node_modules/mermaid/dist"
    with ThreadingHTTPServer(("127.0.0.1", args.port), Handler) as server:
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass


if __name__ == "__main__":
    main()
