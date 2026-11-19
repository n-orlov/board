import { describe, expect, it } from 'vitest';
import { labelNode, type LabelDocument, type LabelElement } from './timelineLabel';

/** A stand-in document: every element records what was written to it. */
class El implements LabelElement {
  className = '';
  textContent: string | null = null;
  attributes: Record<string, string> = {};
  children: El[] = [];
  constructor(readonly tag: string) {}
  setAttribute(name: string, value: string) {
    this.attributes[name] = value;
  }
  appendChild(child: LabelElement) {
    this.children.push(child as El);
  }
}
const doc: LabelDocument = { createElement: (tag) => new El(tag) };

describe('the label of a Timeline row', () => {
  it('is the HTML content as it was for a row with no tags', () => {
    expect(labelNode({ content: 'T1 First', tags: [] }, doc)).toBe('T1 First');
  });

  it('is empty for the group vis-timeline holds the rowless items in, which has no data', () => {
    expect(labelNode(null, doc)).toBe('');
  });

  it('is the title over a line of chips for a row with tags', () => {
    const label = labelNode({ content: 'x', text: 'T1 First', tags: [{ tag: 'urgent', sources: ['task'] }, { tag: 'web', sources: ['initiative', 'actor'] }] }, doc) as El;
    expect(label.className).toBe('tl-label');
    const [title, chips] = label.children;
    expect(title!.className).toBe('tl-label-title');
    expect(title!.textContent).toBe('T1 First');
    expect(title!.children).toHaveLength(0);
    expect(chips!.className).toBe('tl-label-tags');
    const [first, second] = chips!.children;
    expect(first!.className).toBe('vh-tag tl-tag');
    expect(first!.textContent).toBe('urgent');
    expect(first!.attributes['data-source']).toBe('task');
    expect(first!.attributes['data-tag']).toBe('urgent');
    expect(first!.attributes['data-testid']).toBe('task-tag');
    expect(first!.attributes['style']).toMatch(/^--h:\d+$/);
    expect(first!.attributes['title']).toBe('urgent: task tag');
    expect(first!.children[0]!.textContent).toBe(' (task tag)');
    expect(second!.attributes['data-source']).toBe('initiative');
    expect(second!.attributes['title']).toBe('web: initiative tag, actor kind, from the holder of a lease');
  });

  it('puts the attention badge text after the title, in its own element', () => {
    const label = labelNode({ content: 'x', text: 'T1 First', attentionLabel: 'done, not closed', tags: [{ tag: 'a', sources: ['task'] }] }, doc) as El;
    const badge = label.children[0]!.children[0]!;
    expect(badge.className).toBe('tl-attention');
    expect(badge.textContent).toBe(' done, not closed');
  });

  it('tolerates a row with tags and no text', () => {
    const label = labelNode({ content: 'x', tags: [{ tag: 'a', sources: [] }] }, doc) as El;
    expect(label.children[0]!.textContent).toBe('');
    expect(label.children[1]!.children[0]!.attributes['data-source']).toBe('task');
  });
});
