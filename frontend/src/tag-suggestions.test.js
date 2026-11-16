import test from "node:test";
import assert from "node:assert/strict";
import {
  activeSearchFragment,
  moveSuggestionIndex,
  replaceSearchFragment,
  selectedSuggestion,
} from "./tag-suggestions.js";

test("active fragment replaces only the tag at the caret", () => {
  const query = "language:japanese female:catg rating:5";
  const caret = query.indexOf("catg") + 4;
  const fragment = activeSearchFragment(query, caret, caret);
  assert.equal(fragment.text, "female:catg");
  assert.equal(
    replaceSearchFragment(query, fragment, "female:catgirl"),
    "language:japanese female:catgirl rating:5",
  );
});

test("active fragment keeps whitespace inside an open quoted tag", () => {
  const query = 'female:"space tag" male:cat';
  const caret = query.indexOf("tag") + 1;
  const fragment = activeSearchFragment(query, caret, caret);
  assert.equal(fragment.text, 'female:"space tag"');
  assert.equal(
    replaceSearchFragment(query, fragment, 'female:"space tag"'),
    'female:"space tag" male:cat',
  );
});

test("partial selections inside a namespaced tag are marked unsafe to replace", () => {
  const query = "female:catgirl";
  const fragment = activeSearchFragment(query, 7, 10);
  assert.equal(fragment.text, "cat");
  assert.equal(fragment.partialSelection, true);
});

test("selecting a whole namespaced token keeps canonical replacement valid", () => {
  const query = "female:catgirl";
  const fragment = activeSearchFragment(query, 0, query.length);
  assert.equal(fragment.text, query);
  assert.equal(fragment.partialSelection, false);
  assert.equal(
    replaceSearchFragment(query, fragment, "female:kitten"),
    "female:kitten",
  );
});

test("suggestion keyboard navigation wraps and Enter resolves the active item", () => {
  const items = [{ query: "female:catgirl" }, { query: "female:cat" }];
  assert.equal(moveSuggestionIndex(-1, items.length, 1), 0);
  assert.equal(moveSuggestionIndex(0, items.length, -1), 1);
  assert.equal(moveSuggestionIndex(1, items.length, 1), 0);
  assert.equal(selectedSuggestion(items, 1), items[1]);
  assert.equal(selectedSuggestion(items, -1), null);
});
