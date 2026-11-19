// The label of a Timeline row (PRD R9, SPEC 11). vis-timeline draws an HTML label through a sanitizer that drops `class`,
// `style` and `data-` attributes, so a coloured tag chip cannot be written as a string. A `groupTemplate` may return a DOM node
// instead, and that is left as built: `labelNode` builds the two lines of a task row with tags, the title over the chips.
// The document is passed in so a test can hand in a stand-in (the unit tests run without a DOM).
import { chipSourceText, tagStyle, type TagChoice } from './chipFilter';
import { chipTitle } from './taskTags';

/** The part of an element `labelNode` writes. */
export interface LabelElement {
  className: string;
  textContent: string | null;
  setAttribute(name: string, value: string): void;
  appendChild(child: LabelElement): unknown;
}

/** The part of a document `labelNode` reads. */
export interface LabelDocument {
  createElement(tag: string): LabelElement;
}

/** What a row gives the label: its plain text, the attention badge text, its tags and its HTML fallback. */
export interface LabelGroup {
  content: string;
  tags: readonly TagChoice[];
  /** The row's title as plain text (`ref title`); the label is built from it when the row has tags. */
  text?: string;
  attentionLabel?: string;
}

function element(doc: LabelDocument, tag: string, className: string, text?: string): LabelElement {
  const el = doc.createElement(tag);
  el.className = className;
  if (text !== undefined) el.textContent = text;
  return el;
}

/** One tag chip: the tag colour, the source as a data attribute (its border), the title naming its sources and a screen-reader suffix. */
function chipElement(doc: LabelDocument, chip: TagChoice): LabelElement {
  const el = element(doc, 'span', 'vh-tag tl-tag', chip.tag);
  el.setAttribute('style', tagStyle(chip.tag));
  el.setAttribute('data-source', chip.sources.includes('task') ? 'task' : (chip.sources[0] ?? 'task'));
  el.setAttribute('data-tag', chip.tag);
  el.setAttribute('data-testid', 'task-tag');
  el.setAttribute('title', chipTitle(chip));
  el.appendChild(element(doc, 'span', 'sr-only', ` (${chipSourceText(chip)})`));
  return el;
}

/**
 * The label of a row: its HTML `content` as it was for a row with no tags, and for one with tags the title line (with the
 * attention badge text after it, in the danger colour) over a line of chips. A row that is null (vis-timeline's own empty
 * group) has an empty label.
 */
export function labelNode(group: LabelGroup | null, doc: LabelDocument): LabelElement | string {
  // vis-timeline asks for the label of the group that holds the items on no row, which has no data at all.
  if (group === null) return '';
  if (group.tags.length === 0) return group.content;
  const label = element(doc, 'span', 'tl-label');
  const title = element(doc, 'span', 'tl-label-title', group.text ?? '');
  if ((group.attentionLabel ?? '') !== '') {
    title.appendChild(element(doc, 'span', 'tl-attention', ` ${group.attentionLabel}`));
  }
  const chips = element(doc, 'span', 'tl-label-tags');
  for (const chip of group.tags) chips.appendChild(chipElement(doc, chip));
  label.appendChild(title);
  label.appendChild(chips);
  return label;
}
