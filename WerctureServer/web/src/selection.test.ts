import assert from "node:assert/strict";
import test from "node:test";
import { selectPicture, selectionLimit } from "./selection.ts";

test("checkbox and drag selection stay within the server batch limit", () => {
  let selected: number[] = [];
  for (let id = 1; id <= 120; id += 1) selected = selectPicture(selected, id, true);
  assert.equal(selected.length, selectionLimit);
  assert.equal(selected.includes(101), false);
  selected = selectPicture(selected, 1, false);
  selected = selectPicture(selected, 101, true);
  assert.equal(selected.length, selectionLimit);
  assert.equal(selected.includes(101), true);
});

test("dragging across an already selected picture does not duplicate its ID", () => {
  let selected = selectPicture([], 1, true);
  selected = selectPicture(selected, 1, true);
  selected = selectPicture(selected, 2, true);
  assert.deepEqual(selected, [1, 2]);
});
