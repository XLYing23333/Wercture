import assert from "node:assert/strict";
import test from "node:test";
import { renderMarkdown } from "./markdown.ts";

test("math href cannot break out into an event handler", () => {
  const html = renderMarkdown('$$\\href{https://x/"><img src=x onerror=alert(1)>}{click}$$');
  assert.equal(/onerror/i.test(html), false);
  assert.equal(/<script/i.test(html), false);
  assert.equal(/javascript:/i.test(html), false);
});

test("javascript links are removed", () => {
  const html = renderMarkdown("$$\\href{javascript:alert(1)}{click}$$");
  assert.equal(/javascript:/i.test(html), false);
  assert.equal(/<script/i.test(html), false);
});

test("ordinary https links stay", () => {
  const html = renderMarkdown("[note](https://example.com/a)");
  assert.match(html, /href="https:\/\/example\.com\/a"/);
});
