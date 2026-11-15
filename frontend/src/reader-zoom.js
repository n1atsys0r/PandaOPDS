// Adapted from Sumidoku webui/src/pages/reader/pinchZoom.ts at
// 53d823cd560061da3a9f8f43baa8552a9c843c3f (MIT; see THIRD_PARTY_NOTICES.md).
export class PinchZoomController {
  constructor(onTransform) {
    this.onTransform = onTransform;
    this.pointers = new Map();
    this.transform = { s: 1, tx: 0, ty: 0 };
    this.bounds = { width: 0, height: 0 };
    this.mode = "idle";
    this.prevDist = 0;
    this.prevMid = { x: 0, y: 0 };
    this.last = { x: 0, y: 0 };
    this.panTravel = 0;
    this.dirty = false;
  }
  down(id, x, y, rect) {
    if (!this.pointers.size) {
      this.originX = rect.x;
      this.originY = rect.y;
      this.bounds = { width: rect.width, height: rect.height };
      this.last = { x: x - rect.x, y: y - rect.y };
      this.mode = this.transform.s > 1 ? "pan" : "idle";
      this.panTravel = 0;
      this.dirty = false;
    } else if (this.pointers.size === 1) {
      this.mode = "pinch";
      this.dirty = true;
      this.prevDist = 0;
    }
    this.pointers.set(id, { x: x - rect.x, y: y - rect.y });
  }
  move(id, x, y) {
    const p = this.pointers.get(id);
    if (!p) return;
    p.x = x - this.originX;
    p.y = y - this.originY;
    if (this.mode === "pinch") {
      const [a, b] = [...this.pointers.values()];
      if (!a || !b) return;
      const dist = Math.hypot(a.x - b.x, a.y - b.y),
        mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      if (this.prevDist > 0 && dist > 0) {
        const t = this.transform,
          s = Math.min(5, Math.max(1, (t.s * dist) / this.prevDist)),
          k = s / t.s;
        this.apply(
          s,
          mid.x - (mid.x - t.tx - (mid.x - this.prevMid.x)) * k,
          mid.y - (mid.y - t.ty - (mid.y - this.prevMid.y)) * k,
        );
      }
      this.prevDist = dist;
      this.prevMid = mid;
    } else if (this.mode === "pan") {
      const dx = p.x - this.last.x,
        dy = p.y - this.last.y;
      this.last = { x: p.x, y: p.y };
      this.panTravel += Math.abs(dx) + Math.abs(dy);
      this.apply(
        this.transform.s,
        this.transform.tx + dx,
        this.transform.ty + dy,
      );
    }
  }
  up(id) {
    if (!this.pointers.delete(id)) return;
    if (this.pointers.size >= 2) {
      this.prevDist = 0;
      return;
    }
    if (this.pointers.size === 1) {
      const [p] = this.pointers.values();
      this.last = { x: p.x, y: p.y };
      this.prevDist = 0;
      this.mode = this.transform.s > 1 ? "pan" : "idle";
      return;
    }
    if (this.transform.s > 1 && this.transform.s < 1.08) this.apply(1, 0, 0);
    if (this.dirty || this.panTravel > 8) this.suppressClick = true;
    this.mode = "idle";
  }
  reset() {
    this.pointers.clear();
    this.mode = "idle";
    this.prevDist = 0;
    this.panTravel = 0;
    this.dirty = false;
    this.suppressClick = false;
    this.transform = { s: 1, tx: 0, ty: 0 };
    this.onTransform({ ...this.transform });
  }
  setScale(scale, rect) {
    this.bounds = { width: rect.width, height: rect.height };
    this.transform = { s: Math.min(5, Math.max(1, scale)), tx: 0, ty: 0 };
    this.onTransform({ ...this.transform });
  }
  gestureConsumed() {
    return this.dirty || this.panTravel > 8 || this.transform.s > 1;
  }
  consumeClick() {
    const v = this.suppressClick;
    this.suppressClick = false;
    return v;
  }
  apply(s, tx, ty) {
    const { width, height } = this.bounds;
    this.transform = {
      s,
      tx: Math.min(0, Math.max(width * (1 - s), tx)),
      ty: Math.min(0, Math.max(height * (1 - s), ty)),
    };
    this.onTransform({ ...this.transform });
  }
}
