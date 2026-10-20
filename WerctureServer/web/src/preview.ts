import { docError } from "./doc-copy";
import { renderMarkdown } from "./markdown";
import "./preview.css";

const root = document.querySelector("#doc");
const [locale, response] = await Promise.all([siteLocale(), fetch(`${location.pathname}?raw=1`)]);
if (!root) {
  throw new Error("missing document");
}
document.documentElement.lang = locale === "zh-CN" ? "zh-CN" : "en";
if (!response.ok) {
  root.textContent = docError(locale);
} else {
  const name = filename(response.headers.get("content-disposition") || "");
  if (name) document.title = name;
  root.innerHTML = renderMarkdown(await response.text());
}

async function siteLocale() {
  try {
    const response = await fetch("/api/v1/settings");
    if (!response.ok) return "en";
    const body = await response.json();
    return body.locale === "zh-CN" ? "zh-CN" : "en";
  } catch {
    return "en";
  }
}

function filename(header: string) {
  const star = /filename\*=(?:UTF-8|utf-8)''([^;]+)/.exec(header);
  if (star) {
    try {
      return decodeURIComponent(star[1]);
    } catch {
      return star[1];
    }
  }
  const plain = /filename="([^"]*)"/.exec(header);
  return plain ? plain[1] : "";
}
