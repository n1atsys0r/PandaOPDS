export function createScrollLocker(
  doc = typeof document !== "undefined" ? document : null,
) {
  let isLocked = false;
  let savedOverflow = "";

  function lock() {
    if (!doc?.body) return;
    if (isLocked) return;
    const current = doc.body.style.overflow || "";
    savedOverflow = current === "hidden" ? "" : current;
    doc.body.style.overflow = "hidden";
    isLocked = true;
  }

  function unlock() {
    if (!doc?.body) return;
    if (!isLocked && doc.body.style.overflow !== "hidden") return;
    const target =
      savedOverflow && savedOverflow !== "hidden" ? savedOverflow : "";
    doc.body.style.overflow = target;
    savedOverflow = "";
    isLocked = false;
  }

  function sync(shouldLock) {
    if (shouldLock) {
      lock();
    } else {
      unlock();
    }
  }

  function getStatus() {
    return {
      isLocked,
      savedOverflow,
      currentOverflow: doc?.body?.style?.overflow || "",
    };
  }

  return {
    lock,
    unlock,
    sync,
    getStatus,
  };
}
