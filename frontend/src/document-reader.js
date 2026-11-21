export async function prepareDocumentForReading(doc, api) {
  const id = String(doc?.id || "");
  if (!id) throw new Error("document id is required");
  const { data: manifest } = await api(
    `/opds/v2.0/documents/${encodeURIComponent(id)}/manifest`,
  );
  const pageCount = Number(manifest?.metadata?.numberOfPages || 0);
  if (!Number.isFinite(pageCount) || pageCount <= 0) {
    throw new Error("document has no readable pages");
  }
  // Lazy indexing can discover trusted archive identity and change the frozen
  // progression URL. Fetch the document again after manifest construction.
  const { data: latest } = await api(
    `/api/documents/${encodeURIComponent(id)}`,
  );
  return { ...doc, ...latest, page_count: pageCount, document_id: id };
}

export function documentReadErrorMessage(error, t) {
  const reason = error?.data?.reason;
  const message = reason
    ? t(`documents.read_error_${reason}`)
    : error?.message || String(error);
  const diagnostic = error?.data?.path
    ? ` (${error.data.path}; ${JSON.stringify(error.data.stored_signature || {})}${error.data.current_signature ? ` → ${JSON.stringify(error.data.current_signature)}` : ""})`
    : "";
  return `${message}${diagnostic}`;
}
