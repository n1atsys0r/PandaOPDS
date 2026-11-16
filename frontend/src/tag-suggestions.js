function tokenRangeAt(text, caret) {
  let start = 0;
  let quoted = false;
  for (let i = 0; i < caret; i += 1) {
    if (text[i] === '"') quoted = !quoted;
    else if (/\s/.test(text[i]) && !quoted) start = i + 1;
  }

  let end = text.length;
  for (let i = caret; i < text.length; i += 1) {
    if (text[i] === '"') quoted = !quoted;
    else if (/\s/.test(text[i]) && !quoted) {
      end = i;
      break;
    }
  }
  return { start, end };
}

export function activeSearchFragment(value, selectionStart, selectionEnd) {
  const text = String(value || "");
  const startAt = Math.max(
    0,
    Math.min(text.length, selectionStart ?? text.length),
  );
  const endAt = Math.max(
    startAt,
    Math.min(text.length, selectionEnd ?? startAt),
  );
  if (endAt > startAt) {
    const token = tokenRangeAt(text, startAt);
    const fullTokenSelected = startAt === token.start && endAt === token.end;
    return {
      start: startAt,
      end: endAt,
      text: text.slice(startAt, endAt),
      partialSelection: !fullTokenSelected,
    };
  }

  const token = tokenRangeAt(text, startAt);
  return {
    ...token,
    text: text.slice(token.start, token.end),
    partialSelection: false,
  };
}

export function replaceSearchFragment(value, fragment, replacement) {
  const text = String(value || "");
  return `${text.slice(0, fragment.start)}${replacement}${text.slice(fragment.end)}`;
}

export function moveSuggestionIndex(index, count, direction) {
  if (count <= 0) return -1;
  if (index < 0) return direction < 0 ? count - 1 : 0;
  return (index + direction + count) % count;
}

export function selectedSuggestion(suggestions, index) {
  return suggestions[index] || null;
}
