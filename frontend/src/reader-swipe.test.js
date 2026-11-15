import test from "node:test";
import assert from "node:assert/strict";
import {
  decideSwipe,
  releaseVelocity,
  rescuesHorizontal,
  swipeTurnThreshold,
} from "./reader-swipe.js";

test("swipe release commits only a deliberate direction in RTL and LTR", () => {
  const threshold = swipeTurnThreshold(390);
  assert.equal(decideSwipe(40, 0, 1, threshold), "forward");
  assert.equal(decideSwipe(-40, 0, 1, threshold), "backward");
  assert.equal(decideSwipe(-40, 0, -1, threshold), "forward");
  assert.equal(decideSwipe(12, 0, 1, threshold), null);
});

test("short fast flick turns, while a cancelled drag has no decision", () => {
  const threshold = swipeTurnThreshold(390);
  assert.equal(decideSwipe(20, 0.3, 1, threshold), "forward");
  assert.equal(decideSwipe(20, 0, 1, threshold), null);
  assert.equal(decideSwipe(-20, -0.3, -1, threshold), "forward");
});

test("release velocity uses recent travel and diagonal rescue requires clear horizontal intent", () => {
  assert.equal(
    releaseVelocity(
      [
        { x: 0, t: 0 },
        { x: 10, t: 50 },
      ],
      50,
    ),
    0.2,
  );
  const threshold = swipeTurnThreshold(400);
  assert.equal(rescuesHorizontal("x", 60, 30, false, threshold), true);
  assert.equal(rescuesHorizontal("y", 60, 30, false, threshold), true);
  assert.equal(rescuesHorizontal("y", 60, 50, false, threshold), false);
  assert.equal(rescuesHorizontal("y", 100, 5, true, threshold), false);
});
