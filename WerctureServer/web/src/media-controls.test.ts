import test from "node:test";
import assert from "node:assert/strict";
import { mediaTime, seekPosition } from "./media-controls.ts";

test("media times support long files and unknown metadata", () => {
  for (const [value, expected] of [[0, "0:00"], [65.9, "1:05"], [3661, "1:01:01"], [-3, "0:00"], [Infinity, "0:00"], [NaN, "0:00"]] as const) {
    assert.equal(mediaTime(value), expected);
  }
});
test("seek positions stay inside a finite duration", () => {
  assert.equal(seekPosition(20, 12), 12);
  assert.equal(seekPosition(-2, 12), 0);
  assert.equal(seekPosition(5.2, 12), 5.2);
  assert.equal(seekPosition(Infinity, 12), 0);
  assert.equal(seekPosition(2, NaN), 0);
});
