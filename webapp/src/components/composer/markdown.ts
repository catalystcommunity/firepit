// Markdown render + sanitize for the post composer's preview tab (task C2,
// PLANDOC.md §7: "markdown editor + marked/dompurify preview... mirror
// longhouse/webapp's sanitization config"). This is that mirror —
// longhouse/webapp/src/components/MarkdownEditor.tsx's `sanitizeOptions` +
// "no raw inline HTML" renderer override, copied verbatim (same allowlist,
// same anchor-target hook) so the two apps share one security posture for
// user-authored markdown. `Post.bodyMd`/`Comment.bodyMd` are stored and
// transmitted raw (PLANDOC.md §4/§5) — rendering *and* sanitizing are a
// client concern everywhere in this app, this module is that concern for C2.
//
// Security model (unchanged from longhouse):
//   * Raw HTML typed by the user is NOT rendered — the renderer hook below
//     HTML-escapes any `<...>` token instead of emitting it, so a user-typed
//     `<script>` or `<a href="evil">` shows up as literal text.
//   * `<a>` IS in the sanitizer's allowlist, but only markdown-syntax
//     anchors reach it (`[text](url)`/autolinks) — inline HTML is already
//     defused above, so DOMPurify never sees a hand-typed `<a>`.
//   * DOMPurify is still the last line of defense: it strips anything
//     outside the structural-markdown allowlist below.
//   * External links get `target="_blank" rel="noopener noreferrer"` so a
//     user-supplied link can't hijack the SPA tab via `window.opener`.
import { marked } from "marked";
import DOMPurify, { type Config } from "dompurify";

marked.setOptions({ gfm: true, breaks: true });

const escapeHTML = (s: string): string => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

marked.use({
  renderer: {
    html(token: string | { text?: string; raw?: string }) {
      const src = typeof token === "string" ? token : (token.text ?? token.raw ?? "");
      return escapeHTML(src);
    },
  },
});

const sanitizeOptions: Config = {
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

DOMPurify.removeAllHooks();
DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  if (node.tagName !== "A") return;
  const href = node.getAttribute("href") ?? "";
  let external = true;
  try {
    const base = typeof window !== "undefined" ? window.location.href : "http://localhost";
    const u = new URL(href, base);
    external = typeof window !== "undefined" ? u.origin !== window.location.origin : true;
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

/** Render + sanitize a markdown body for preview. Blank/whitespace-only input renders as `""`. */
export function renderMarkdown(bodyMd: string): string {
  const trimmed = bodyMd.trim();
  if (!trimmed) return "";
  const html = marked.parse(trimmed, { async: false }) as string;
  return String(DOMPurify.sanitize(html, sanitizeOptions));
}
