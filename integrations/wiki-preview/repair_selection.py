# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Select published English prose for module-owned Chinese regeneration."""

import re
from datetime import datetime


def prose_language(markdown):
    text = re.sub(r"(?ms)^(`{3,}|~{3,}).*?^\1[^\n]*$", "", markdown)
    text = re.sub(r"`[^`\n]+`|https?://\S+", "", text)
    text = "\n".join(line for line in text.splitlines()
                     if not line.lstrip().startswith(("#", "|", ">")))
    chinese = len(re.findall(r"[\u3400-\u9fff]", text))
    latin = len(re.findall(r"[A-Za-z]", text))
    total = chinese + latin
    if chinese >= 100 or (chinese >= 20 and chinese / max(total, 1) >= 0.15):
        return "zh"
    if latin >= 80 and chinese / max(total, 1) <= 0.03:
        return "en"
    return "unknown"


def select_pages(pages):
    selected = {"en": [], "zh": [], "unknown": []}
    for page in pages:
        selected[prose_language(page["markdown"])].append(page["slug"])
    return selected


def resumable_page(page, since):
    if page.get("status") != "generated" or not page.get("updated_at"):
        return False
    try:
        updated = datetime.fromisoformat(page["updated_at"].replace("Z", "+00:00"))
    except ValueError:
        return False
    if updated.tzinfo is None or updated.timestamp() < since:
        return False
    return prose_language(page["markdown"]) == "zh"
