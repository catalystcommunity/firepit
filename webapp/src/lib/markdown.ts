// Shared markdown render+sanitize utility (task C3, PLANDOC.md §7: "markdown
// body (marked + dompurify, mirror longhouse's sanitization config)").
// `Post.bodyMd`/`Comment.bodyMd` are raw markdown stored/transmitted as-is
// (types.gen.ts: "rendering (and sanitizing) happens client-side") — this is
// the one place that happens, so every rendered body and every composer
// preview goes through `renderMarkdown` and nothing else calls `marked` or
// `DOMPurify` directly.
//
// Security model (ported from longhouse's webapp/src/components/
// MarkdownEditor.tsx — same intent, adapted to firepit's needs):
//   - Raw HTML typed by a user is NOT rendered. `marked`'s `html` renderer
//     hook is overridden to HTML-escape instead of pass through, so a
//     user-typed `<script>` or `<a href=javascript:...>` shows up as literal
//     text rather than a tag DOMPurify then has to catch.
//   - `<a>` tags ARE allowed in the sanitized output — but only the ones
//     `marked` itself emits from markdown syntax (`[text](url)`, autolinks),
//     since inline HTML is disabled above.
//   - DOMPurify is still the last line of defense: a tight `ALLOWED_TAGS`/
//     `ALLOWED_ATTR` allowlist (structural markdown + anchors) drops
//     anything else by omission (script/style/iframe/form/object/embed/…),
//     and `ALLOWED_URI_REGEXP` restricts hrefs to http(s)/mailto.
//   - External links get `target="_blank" rel="noopener noreferrer"` (can't
//     hijack the SPA tab via `window.opener`); same-origin links are left
//     alone so in-app anchors (once any exist) navigate normally.
import { marked } from "marked";
import DOMPurify, { type Config } from "dompurify";

marked.setOptions({ gfm: true, breaks: true });

const escapeHtml = (s: string): string => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

marked.use({
  renderer: {
    html(token: unknown) {
      const t = token as { text?: string; raw?: string } | string;
      const src = typeof t === "string" ? t : (t.text ?? t.raw ?? "");
      return escapeHtml(src);
    },
  },
});

const SANITIZE_OPTIONS: Config = {
  ALLOWED_TAGS: [
    "p",
    "br",
    "hr",
    "strong",
    "em",
    "del",
    "code",
    "pre",
    "ul",
    "ol",
    "li",
    "blockquote",
    "a",
    "h1",
    "h2",
    "h3",
    "h4",
    "h5",
    "h6",
    "table",
    "thead",
    "tbody",
    "tr",
    "th",
    "td",
  ],
  ALLOWED_ATTR: ["href", "title", "align", "target", "rel"],
  ALLOWED_URI_REGEXP: /^(?:https?|mailto):/i,
};

let hooksInstalled = false;
function installLinkHook(): void {
  if (hooksInstalled) return;
  hooksInstalled = true;
  DOMPurify.addHook("afterSanitizeAttributes", (node) => {
    if (node.tagName !== "A") return;
    const href = node.getAttribute("href") ?? "";
    let external = true;
    try {
      const u = new URL(href, typeof window !== "undefined" ? window.location.href : "http://localhost");
      external = typeof window === "undefined" || u.origin !== window.location.origin;
    } catch {
      external = false;
    }
    if (external) {
      node.setAttribute("target", "_blank");
      node.setAttribute("rel", "noopener noreferrer");
    } else {
      node.removeAttribute("target");
      node.removeAttribute("rel");
    }
  });
}

/**
 * Render markdown source to sanitized HTML, safe to assign via `innerHTML`.
 * Empty/whitespace-only input renders to `""` (callers can `<Show>` around
 * that instead of rendering an empty `<p>`).
 */
export function renderMarkdown(source: string): string {
  const trimmed = source.trim();
  if (!trimmed) return "";
  installLinkHook();
  const html = marked.parse(trimmed, { async: false }) as string;
  // DOMPurify.sanitize returns TrustedHTML when Trusted Types are enforced;
  // treat it as a plain string either way for innerHTML assignment.
  return String(DOMPurify.sanitize(html, SANITIZE_OPTIONS));
}

/**
 * `@handle` tokens appearing in a chunk of markdown source (mentions are
 * plain text, not a distinct AST node — see MentionAutocomplete's doc
 * comment for why this is the only mention-discovery mechanism v1 has).
 */
const MENTION_RE = /@([a-zA-Z][a-zA-Z0-9_-]{0,30})/g;

export function extractMentionHandles(source: string): string[] {
  const seen = new Set<string>();
  for (const match of source.matchAll(MENTION_RE)) {
    seen.add(match[1].toLowerCase());
  }
  return [...seen];
}
