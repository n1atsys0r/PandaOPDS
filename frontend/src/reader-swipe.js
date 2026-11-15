// Adapted from Sumidoku webui/src/pages/reader/swipeTurn.ts at
// 53d823cd560061da3a9f8f43baa8552a9c843c3f (MIT; see THIRD_PARTY_NOTICES.md).
export const SWIPE_TURN_FRACTION = 0.08;
export const SWIPE_TURN_MIN_PX = 32;
export const SWIPE_TURN_MAX_PX = 56;
export const SWIPE_FLICK_PX_PER_MS = 0.2;
export const SWIPE_FLICK_MIN_PX = 16;
export const SWIPE_RESCUE_RATIO = 1.25;
export const SWIPE_VELOCITY_WINDOW_MS = 100;

export function swipeTurnThreshold(width) {
  return Math.min(
    Math.max(width * SWIPE_TURN_FRACTION, SWIPE_TURN_MIN_PX),
    SWIPE_TURN_MAX_PX,
  );
}

export function releaseVelocity(trail, now) {
  if (trail.length < 2) return 0;
  const from = now - SWIPE_VELOCITY_WINDOW_MS;
  let base = trail[0];
  for (const sample of trail) {
    if (sample.t > from) break;
    base = sample;
  }
  const span = now - Math.max(base.t, from);
  return span > 0 ? (trail[trail.length - 1].x - base.x) / span : 0;
}

export function decideSwipe(offset, velocity, forwardSign, threshold) {
  const fwdOffset = offset * forwardSign;
  const fwdVelocity = velocity * forwardSign;
  if (fwdOffset >= threshold) return "forward";
  if (fwdOffset <= -threshold) return "backward";
  if (fwdVelocity >= SWIPE_FLICK_PX_PER_MS && fwdOffset >= SWIPE_FLICK_MIN_PX)
    return "forward";
  if (fwdVelocity <= -SWIPE_FLICK_PX_PER_MS && fwdOffset <= -SWIPE_FLICK_MIN_PX)
    return "backward";
  return null;
}

export function rescuesHorizontal(axis, dx, dy, scrolled, threshold) {
  if (axis === "x") return true;
  if (axis !== "y" || scrolled) return false;
  return (
    Math.abs(dx) >= threshold &&
    Math.abs(dx) >= SWIPE_RESCUE_RATIO * Math.abs(dy)
  );
}
