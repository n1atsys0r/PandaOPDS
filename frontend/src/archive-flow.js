export function archiveActionFor(status) {
  if (["ready", "unavailable"].includes(status)) return "manage";
  if (["pending", "submitting", "staging", "downloading", "zipping"].includes(status)) return "busy";
  return "quote";
}
