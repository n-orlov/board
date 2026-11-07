import { describe, expect, it } from 'vitest';
import { CADENCE_HINT, addLink, dayInstant, emptyNewTask, parseCadence, pickerOptions, removeLink, toTaskCreate, toggleBlocker, type NewTaskForm } from './newTask';

const form = (extra: Partial<NewTaskForm> = {}): NewTaskForm => ({ ...emptyNewTask('aa'), title: ' Write it ', ...extra });

describe('emptyNewTask', () => {
  it('starts blank as a work task in the preset initiative', () => {
    expect(emptyNewTask('aa')).toEqual({
      initiative: 'aa',
      title: '',
      description: '',
      kind: 'work',
      cadence: '',
      plannedStart: '',
      plannedEnd: '',
      blockedBy: [],
      links: [],
      linkLabel: '',
      linkUrl: '',
    });
    expect(emptyNewTask('').initiative).toBe('');
  });
});

describe('parseCadence', () => {
  it.each([
    ['90s', 90],
    ['15m', 900],
    [' 2h ', 7200],
    ['1d', 86400],
    ['3 M', 180],
  ])('reads %j as %d seconds', (text, seconds) => {
    expect(parseCadence(text)).toBe(seconds);
  });

  it.each([[''], ['15'], ['m'], ['1w'], ['1.5h'], ['-5m'], ['5m5s'], ['every 5m']])('rejects %j', (text) => {
    expect(parseCadence(text)).toBeUndefined();
  });
});

describe('dayInstant', () => {
  it('is the start of the day in UTC', () => {
    expect(dayInstant(' 2026-03-01 ')).toBe('2026-03-01T00:00:00Z');
  });
});

describe('toTaskCreate', () => {
  it('sends the trimmed title, the initiative and the kind, and nothing blank', () => {
    expect(toTaskCreate(form())).toEqual({ ok: true, body: { initiative: 'aa', title: 'Write it', kind: 'work' } });
  });

  it('keeps a trimmed description, the planned days, the blockers and the links', () => {
    const result = toTaskCreate(
      form({
        description: ' why ',
        plannedStart: '2026-03-01',
        plannedEnd: '2026-03-09',
        blockedBy: ['aa-1', 'bb-2'],
        links: [{ label: 'PR', url: 'https://example.com/1' }],
      }),
    );
    expect(result).toEqual({
      ok: true,
      body: {
        initiative: 'aa',
        title: 'Write it',
        kind: 'work',
        description: 'why',
        planned_start: '2026-03-01T00:00:00Z',
        planned_end: '2026-03-09T00:00:00Z',
        blocked_by: ['aa-1', 'bb-2'],
        external_refs: [{ label: 'PR', url: 'https://example.com/1' }],
      },
    });
  });

  it('sends a watch task with its cadence in seconds', () => {
    expect(toTaskCreate(form({ kind: 'watch', cadence: '15m' }))).toEqual({
      ok: true,
      body: { initiative: 'aa', title: 'Write it', kind: 'watch', cadence_seconds: 900 },
    });
  });

  it('refuses a watch task whose cadence is not a number and a unit', () => {
    expect(toTaskCreate(form({ kind: 'watch', cadence: 'often' }))).toEqual({ ok: false, error: CADENCE_HINT });
    expect(toTaskCreate(form({ kind: 'watch' }))).toEqual({ ok: false, error: CADENCE_HINT });
  });

  it('does not send a cadence typed before the kind went back to work', () => {
    expect(toTaskCreate(form({ cadence: '15m' }))).toEqual({ ok: true, body: { initiative: 'aa', title: 'Write it', kind: 'work' } });
  });

  it('adds a link that was typed but not added', () => {
    const result = toTaskCreate(form({ links: [{ label: 'A', url: 'https://a.example' }], linkLabel: ' B ', linkUrl: ' https://b.example ' }));
    expect(result).toMatchObject({ ok: true, body: { external_refs: [{ label: 'A', url: 'https://a.example' }, { label: 'B', url: 'https://b.example' }] } });
  });

  it('refuses a link with only one part typed', () => {
    const error = { ok: false, error: 'A link needs both a label and a URL.' };
    expect(toTaskCreate(form({ linkLabel: 'B' }))).toEqual(error);
    expect(toTaskCreate(form({ linkUrl: 'https://b.example' }))).toEqual(error);
  });

  it('does not share the arrays of the form with the body', () => {
    const f = form({ blockedBy: ['aa-1'], links: [{ label: 'A', url: 'https://a.example' }] });
    const result = toTaskCreate(f);
    if (!result.ok) throw new Error('refused');
    expect(result.body.blocked_by).not.toBe(f.blockedBy);
    expect(result.body.external_refs?.[0]).not.toBe(f.links[0]);
  });
});

describe('addLink and removeLink', () => {
  it('moves a complete draft into the list and clears it', () => {
    const f = addLink(form({ linkLabel: ' PR ', linkUrl: ' https://a.example ' }));
    expect(f.links).toEqual([{ label: 'PR', url: 'https://a.example' }]);
    expect([f.linkLabel, f.linkUrl]).toEqual(['', '']);
  });

  it('leaves the form as it is when the draft is blank or half typed', () => {
    const blank = form();
    expect(addLink(blank)).toBe(blank);
    const half = form({ linkLabel: 'PR' });
    expect(addLink(half)).toBe(half);
  });

  it('removes the link at an index and keeps the others', () => {
    const f = form({ links: [{ label: 'A', url: 'u1' }, { label: 'B', url: 'u2' }, { label: 'C', url: 'u3' }] });
    expect(removeLink(f, 1).links.map((l) => l.label)).toEqual(['A', 'C']);
  });
});

describe('toggleBlocker', () => {
  it('adds a ref once and removes it the second time', () => {
    const one = toggleBlocker(form(), 'aa-1');
    expect(one.blockedBy).toEqual(['aa-1']);
    const two = toggleBlocker(one, 'bb-2');
    expect(two.blockedBy).toEqual(['aa-1', 'bb-2']);
    expect(toggleBlocker(two, 'aa-1').blockedBy).toEqual(['bb-2']);
  });
});

describe('pickerOptions', () => {
  const tasks = [
    { ref: 'aa-1', title: 'Write docs' },
    { ref: 'aa-2', title: 'Ship it' },
    { ref: 'bb-1', title: 'Docs review' },
  ];

  it('offers the tasks whose ref or title holds the text, ignoring case', () => {
    expect(pickerOptions(tasks, ' DOCS ', []).map((t) => t.ref)).toEqual(['aa-1', 'bb-1']);
    expect(pickerOptions(tasks, 'BB-', []).map((t) => t.ref)).toEqual(['bb-1']);
    expect(pickerOptions(tasks, 'zzz', [])).toEqual([]);
  });

  it('offers everything for blank text, minus the chosen ones, up to the limit', () => {
    expect(pickerOptions(tasks, '', ['aa-2']).map((t) => t.ref)).toEqual(['aa-1', 'bb-1']);
    expect(pickerOptions(tasks, '  ', [], 2).map((t) => t.ref)).toEqual(['aa-1', 'aa-2']);
  });
});
