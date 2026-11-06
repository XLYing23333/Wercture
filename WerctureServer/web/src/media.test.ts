import assert from "node:assert/strict";
import test from "node:test";
import { pageHTML, pageMarkdown, type Picture } from "./api.ts";

Object.defineProperty(globalThis, "window", { value: { location: { origin: "https://example.test" } }, configurable: true });

test("audio and video copies use links and escaped media elements", () => {
  for (const [mime, tag] of [["audio/mpeg", "audio"], ["video/mp4", "video"]]) {
    const picture = { mime, path: "/i/abc.mp4", name: 'x]\n"<&>.mp4' } as Picture;
    assert.equal(pageMarkdown(picture), '[x "<&>.mp4](https://example.test/i/abc.mp4)');
    assert.equal(pageHTML(picture), `<${tag} controls src="https://example.test/i/abc.mp4" title="x]&quot;&lt;&amp;&gt;.mp4"></${tag}>`);
  }
});

test("image copies retain image embeds", () => {
  const picture = { mime: "image/png", path: "/i/abc.png", name: "a.png" } as Picture;
  assert.equal(pageMarkdown(picture), "![a.png](https://example.test/i/abc.png)");
  assert.equal(pageHTML(picture), '<img src="https://example.test/i/abc.png" alt="a.png">');
});
