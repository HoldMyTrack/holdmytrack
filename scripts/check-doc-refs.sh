#!/usr/bin/env bash
# Fails when a §N.M, FR-N.M or ADR-NNNN citation points at a heading or ADR that no longer exists.
#
# A citation belongs to the document named before it in the same sentence or parenthesis
# (`docs/SPEC.md` FR-4.2, IMPLEMENTATION §4.7). Without one, a Markdown file cites itself, and
# any other file (code, templates) cites docs/IMPLEMENTATION.md for § and docs/SPEC.md for FR.
# A bare name like `SPEC.md` means the file of that name beside the citing one if there is one,
# else the one in docs/. Usage: scripts/check-doc-refs.sh [file...] (default: every tracked text file).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

if [ "$#" -eq 0 ]; then
  set -- $(git ls-files | grep -vE '^docs/adr/|\.(png|jpg|jpeg|gif|webp|ico|pmtiles|pbf|woff2?|ttf|otf|jar|gz|zip|fit|tcx|gpx|keystore)$|(^|/)package-lock\.json$|^DOC_REWORK\.md$')
fi

python3 - "$@" <<'PY'
import os, re, sys

DOC_NAMES = ["VISION", "ARCHITECTURE", "IMPLEMENTATION", "SPEC", "DEPLOY", "DEVELOPMENT", "ROADMAP",
             "DPIA", "PERFORMANCE", "BRAINSTORM", "KNOWN_ISSUES"]
heading_cache = {}

def headings(path):
    """The § and FR ids a document's headings define."""
    if path in heading_cache:
        return heading_cache[path]
    secs, frs = set(), set()
    if os.path.exists(path):
        fence = False
        for line in open(path, encoding="utf-8"):
            if line.startswith("```"):
                fence = not fence
            if fence or not line.startswith("#"):
                continue
            text = line.lstrip("#").strip()
            # "3. FR-1 — …", "3.14 `x` / 3.15 `y`", "FR-1.1 Sign up", "4.2.6 Caching"
            for m in re.finditer(r"(?:^|/\s*)(\d+(?:\.\d+)*)\.?(?=\s|$)", text):
                secs.add(m.group(1))
            m = re.match(r"(?:\d+\.\s+)?FR-(\d+(?:\.\d+)*)\b", text)
            if m:
                frs.add(m.group(1))
    heading_cache[path] = (secs, frs)
    return secs, frs

def resolve(name, citing):
    """`apps/android/docs/SPEC.md`, `docs/SPEC.md`, `SPEC.md` or `SPEC` → a path."""
    if name in EXTERNAL:
        return None
    name = name.strip("`")
    if not name.endswith(".md"):
        name += ".md"
    if "/" in name:
        if name.startswith("../") or name.startswith("./"):
            p = os.path.normpath(os.path.join(os.path.dirname(citing), name))
            if os.path.exists(p):
                return p
        for prefix in ("", "docs/"):
            if os.path.exists(prefix + name):
                return prefix + name
        return name.lstrip("./")
    beside = os.path.join(os.path.dirname(citing), name)
    if citing.endswith(".md") and os.path.exists(beside):
        return beside
    if citing.startswith("apps/android/") and os.path.exists("apps/android/docs/" + name):
        return "apps/android/docs/" + name
    return "docs/" + name

def default_target(kind, citing):
    """Where a citation with no document named before it points."""
    if kind == "sec":
        return citing if citing.endswith(".md") else "docs/IMPLEMENTATION.md"
    if citing.endswith(".md") and headings(citing)[1]:
        return citing
    if citing.startswith("apps/android/"):
        return "apps/android/docs/SPEC.md"
    return "docs/SPEC.md"

# Other specifications' sections (OpenID Connect Core §3.1.3.7) are not checked.
EXTERNAL = {"OpenID", "RFC"}
doc_re = r"(?:`?(?:[\w./-]*/)?(?:" + "|".join(DOC_NAMES) + r")(?:\.md)?`?|OpenID|RFC)"
token_re = re.compile(
    r"(?P<doc>" + doc_re + r")"
    r"|§(?P<sec>\d+(?:\.\d+)*)"
    r"|\bFR-(?P<fr>\d+(?:\.\d+)*)"
    r"|\bADR-(?P<adr>\d{4})\b"
    r"|(?P<open>\()|(?P<close>\))|(?P<stop>[.;](?:\s|$))"
)
adrs = {f[:4] for f in os.listdir("docs/adr") if re.match(r"\d{4}-", f)}

EXT = object()
failures = 0
for path in sys.argv[1:]:
    if not os.path.isfile(path):
        continue
    try:
        lines = open(path, encoding="utf-8").read().split("\n")
    except UnicodeDecodeError:
        continue
    for n, line in enumerate(lines, 1):
        current, depth, doc_depth = None, 0, 0
        for m in token_re.finditer(line):
            k = m.lastgroup
            if k == "open":
                depth += 1
            elif k == "close":
                depth -= 1
                if current and depth < doc_depth:
                    current = None
            elif k == "stop":
                current = None
            elif k == "doc":
                current, doc_depth = resolve(m.group("doc"), path) or EXT, depth
            elif k == "adr":
                if m.group("adr") not in adrs:
                    print(f"{path}:{n}: ADR-{m.group('adr')} does not exist")
                    failures += 1
            else:
                if current is EXT:
                    continue
                target = current or default_target(k, path)
                if not target.endswith(".md") or not os.path.exists(target):
                    continue
                secs, frs = headings(target)
                ident = m.group(k)
                if k == "fr" and not current:
                    # An app's own docs and code also cite the root spec's FRs bare.
                    frs = frs | headings("docs/SPEC.md")[1]
                if (k == "sec" and ident not in secs) or (k == "fr" and ident not in frs):
                    label = "§" + ident if k == "sec" else "FR-" + ident
                    print(f"{path}:{n}: {label} has no heading in {target}")
                    failures += 1
if failures:
    print(f"{failures} broken citation(s)", file=sys.stderr)
    sys.exit(1)
PY