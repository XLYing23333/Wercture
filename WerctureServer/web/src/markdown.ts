import MarkdownIt from "markdown-it";
import multimdTable from "markdown-it-multimd-table";
import mathjax3 from "markdown-it-mathjax3";

const markdown = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: false,
  typographer: true,
});
markdown.use(multimdTable);
markdown.use(mathjax3);

const openLink = markdown.renderer.rules.link_open;
markdown.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  tokens[idx].attrSet("target", "_blank");
  tokens[idx].attrSet("rel", "noopener noreferrer");
  if (openLink) return openLink(tokens, idx, options, env, self);
  return self.renderToken(tokens, idx, options);
};

export function renderMarkdown(source: string) {
  return sanitizeHTML(markdown.render(source));
}

const droppedTags = new Set(["script", "iframe", "object", "embed", "base"]);
const urlAttrs = new Set(["href", "src", "xlink:href", "action", "formaction"]);

export function sanitizeHTML(input: string) {
  let out = "";
  let i = 0;
  while (i < input.length) {
    const lt = input.indexOf("<", i);
    if (lt < 0) {
      out += input.slice(i);
      break;
    }
    out += input.slice(i, lt);
    if (input.startsWith("<!--", lt)) {
      const end = input.indexOf("-->", lt + 4);
      i = end < 0 ? input.length : end + 3;
      continue;
    }
    const tag = parseTag(input, lt);
    if (!tag) {
      out += "&lt;";
      i = lt + 1;
      continue;
    }
    const name = tag.name.toLowerCase();
    if (droppedTags.has(name)) {
      i = tag.closing || tag.self ? tag.end : skipUntilClose(input, tag.end, name);
      continue;
    }
    out += renderTag(tag);
    i = tag.end;
  }
  return out;
}

type Tag = {
  name: string;
  closing: boolean;
  self: boolean;
  attrs: { name: string; value: string }[];
  end: number;
};

function parseTag(input: string, start: number): Tag | null {
  if (input[start] !== "<") return null;
  let i = start + 1;
  let closing = false;
  if (input[i] === "/") {
    closing = true;
    i += 1;
  }
  const nameStart = i;
  while (i < input.length && /[A-Za-z0-9:_-]/.test(input[i])) i += 1;
  if (i === nameStart) return null;
  const name = input.slice(nameStart, i);
  const attrs: { name: string; value: string }[] = [];
  while (i < input.length) {
    while (i < input.length && /\s/.test(input[i])) i += 1;
    if (i >= input.length) return null;
    if (input[i] === ">") return { name, closing, self: false, attrs, end: i + 1 };
    if (input[i] === "/" && input[i + 1] === ">") return { name, closing, self: true, attrs, end: i + 2 };
    const attrStart = i;
    while (i < input.length && /[^\s=/>]/.test(input[i])) i += 1;
    const attrName = input.slice(attrStart, i);
    if (!attrName) return null;
    while (i < input.length && /\s/.test(input[i])) i += 1;
    let value = "";
    if (input[i] === "=") {
      i += 1;
      while (i < input.length && /\s/.test(input[i])) i += 1;
      if (input[i] === '"' || input[i] === "'") {
        const quote = input[i];
        i += 1;
        const valueStart = i;
        while (i < input.length && input[i] !== quote) i += 1;
        value = input.slice(valueStart, i);
        if (input[i] === quote) i += 1;
      } else {
        const valueStart = i;
        while (i < input.length && !/[\s>]/.test(input[i])) i += 1;
        value = input.slice(valueStart, i);
      }
    }
    attrs.push({ name: attrName, value });
  }
  return null;
}

function renderTag(tag: Tag) {
  if (tag.closing) return `</${tag.name}>`;
  let out = `<${tag.name}`;
  for (const attr of tag.attrs) {
    const name = attr.name.toLowerCase();
    if (name.startsWith("on")) continue;
    if (urlAttrs.has(name) && !safeURL(attr.value)) continue;
    out += ` ${attr.name}="${escapeAttr(attr.value)}"`;
  }
  out += tag.self ? " />" : ">";
  return out;
}

function skipUntilClose(input: string, from: number, name: string) {
  const end = input.toLowerCase().indexOf(`</${name}`, from);
  if (end < 0) return input.length;
  const close = input.indexOf(">", end);
  return close < 0 ? input.length : close + 1;
}

function safeURL(value: string) {
  const decoded = decodeEntities(value);
  if (!decoded || /[<>"'`\s]/.test(decoded) || decoded.startsWith("//") || decoded.startsWith("/\\")) return false;
  if (decoded.startsWith("#") || decoded.startsWith("/") || decoded.startsWith("?") || decoded.startsWith("./") || decoded.startsWith("../")) {
    return true;
  }
  const scheme = /^([a-zA-Z][a-zA-Z0-9+.-]*):/.exec(decoded);
  if (!scheme) return true;
  const name = scheme[1].toLowerCase();
  return name === "http" || name === "https";
}

function decodeEntities(value: string) {
  let out = value;
  for (let pass = 0; pass < 3; pass += 1) {
    const next = out
      .replace(/&amp;|&#0*38;|&#x0*26;/gi, "&")
      .replace(/&quot;|&#0*34;|&#x0*22;/gi, '"')
      .replace(/&lt;|&#0*60;|&#x0*3c;/gi, "<")
      .replace(/&gt;|&#0*62;|&#x0*3e;/gi, ">")
      .replace(/&colon;|&#0*58;|&#x0*3a;/gi, ":");
    if (next === out) break;
    out = next;
  }
  return out.replace(/[\u0000-\u001F\u007F]/g, "").trim();
}

function escapeAttr(value: string) {
  return value.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}
