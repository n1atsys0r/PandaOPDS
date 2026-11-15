import test from "node:test";
import assert from "node:assert/strict";
import { PinchZoomController } from "./reader-zoom.js";

test("an unzoomed single-pointer drag does not become image panning", () => {
  let value;
  const zoom = new PinchZoomController((next) => {
    value = next;
  });
  const rect = { x: 0, y: 0, width: 390, height: 844 };
  zoom.down(1, 250, 400, rect);
  zoom.move(1, 180, 400);
  assert.equal(zoom.gestureConsumed(), false);
  zoom.up(1);
  assert.equal(zoom.transform.s, 1);
  assert.equal(value, undefined);
});

test("pinch zoom scales within bounds and pan remains inside image bounds", () => {
  let value;
  const zoom = new PinchZoomController((next) => {
    value = next;
  });
  const rect = { x: 0, y: 0, width: 200, height: 300 };
  zoom.down(1, 50, 100, rect);
  zoom.down(2, 150, 100, rect);
  zoom.move(2, 250, 100);
  zoom.move(2, 350, 100);
  zoom.move(2, 450, 100);
  zoom.up(2);
  zoom.up(1);
  assert.equal(value.s, 2);
  assert.equal(zoom.gestureConsumed(), true);
  zoom.down(3, 100, 100, rect);
  zoom.move(3, 500, 500);
  zoom.up(3);
  assert.equal(value.tx, 0);
  assert.equal(value.ty, 0);
  zoom.reset();
  assert.deepEqual(value, { s: 1, tx: 0, ty: 0 });
});
