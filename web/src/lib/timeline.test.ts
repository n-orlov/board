import { describe, expect, it } from 'vitest';
import {
  CLOSED_WITHIN_OPTIONS,
  DEFAULT_TIMELINE_FILTER,
  MARKER_GLYPHS,
  actualItemId,
  itemClass,
  apiQuery,
  buildTimeline,
  escapeHtml,
  filterFromHash,
  filterFromQuery,
  filterToQuery,
  isFiltered,
  leaseText,
  taskOfGroup,
  timelineHash,
  type Timeline,
  type TimelineFilter,
} from './timeline';

const NOW = new Date('2026-03-10T12:00:00Z');

function answer(over: Partial<Timeline> = {}): Timeline {
  return {
    from: '2026-02-24T12:00:00Z',
    to: '2026-03-17T12:00:00Z',
    initiatives: [{ slug: 'alpha', title: 'Alpha', tags: ['work', 'home'], closed: false }],
    tasks: [
      {
        ref: 'T1',
        initiative: 'alpha',
        title: 'First',
        status: 'open',
        kind: 'work',
        planned_start: '2026-03-01T00:00:00Z',
        planned_end: '2026-03-12T00:00:00Z',
        created_at: '2026-02-28T09:00:00Z',
        attention: [],
        deps: [],
      },
    ],
    leases: [],
    markers: [],
    ...over,
  };
}

describe('groups', () => {
  it('nests the tasks of an initiative in its collapsible group, in order', () => {
    const t = answer({
      initiatives: [
        { slug: 'alpha', title: 'Alpha', tags: ['work'], closed: false },
        { slug: 'beta', title: 'Beta', tags: [], closed: true },
      ],
      tasks: [
        { ...answer().tasks[0]!, ref: 'T1', initiative: 'alpha' },
        { ...answer().tasks[0]!, ref: 'T2', initiative: 'beta' },
        { ...answer().tasks[0]!, ref: 'T3', initiative: 'alpha' },
      ],
    });
    const { groups } = buildTimeline(t, NOW);
    expect(groups.map((g) => g.id)).toEqual(['i:alpha', 't:T1', 't:T3', 'i:beta', 't:T2']);
    expect(groups.map((g) => g.order)).toEqual([0, 1, 2, 3, 4]);
    expect(groups[0]).toMatchObject({ nestedGroups: ['t:T1', 't:T3'], showNested: true, content: 'Alpha', title: 'alpha (work)', ref: 'alpha' });
    expect(groups[3]!.className).toBe('tl-initiative tl-initiative-closed');
    expect(groups[0]!.className).toBe('tl-initiative');
    expect(groups[3]!.title).toBe('beta');
    expect(groups[1]).toMatchObject({ content: 'T1 First', className: 'tl-task', title: 'T1 First', attentionLabel: '', ref: 'T1' });
    expect(groups[1]!.subgroupStack).toEqual({ planned: false, actual: false, lease: true });
  });

  it('holds a task of an initiative the answer does not list under a group named by its slug', () => {
    const t = answer({ initiatives: [] });
    const { groups } = buildTimeline(t, NOW);
    expect(groups.map((g) => g.id)).toEqual(['i:alpha', 't:T1']);
    expect(groups[0]).toMatchObject({ content: 'alpha', title: 'alpha', className: 'tl-initiative' });
  });

  it('escapes the text vis-timeline draws as HTML', () => {
    const t = answer({ tasks: [{ ...answer().tasks[0]!, title: '<b>x</b> & "y"' }] });
    const { groups, items } = buildTimeline(t, NOW);
    expect(groups[1]!.content).toBe('T1 &lt;b&gt;x&lt;/b&gt; &amp; &quot;y&quot;');
    expect(items.find((i) => i.kind === 'actual')!.content).toBe('&lt;b&gt;x&lt;/b&gt; &amp; &quot;y&quot;');
    expect(escapeHtml("it's")).toBe('it&#39;s');
  });

  it('is empty for an empty answer', () => {
    expect(buildTimeline(answer({ initiatives: [], tasks: [] }), NOW)).toEqual({ groups: [], items: [], dependencies: [] });
  });
});

describe('planned and actual bars', () => {
  it('draws the planned bar and the actual span to now for an open task', () => {
    const { items } = buildTimeline(answer(), NOW);
    expect(items.map((i) => i.id)).toEqual(['plan:T1', 'act:T1']);
    expect(items[0]).toMatchObject({
      group: 't:T1',
      kind: 'planned',
      type: 'range',
      start: '2026-03-01T00:00:00Z',
      end: '2026-03-12T00:00:00Z',
      className: 'tl-planned',
      subgroup: 'planned',
      task: 'T1',
    });
    expect(items[1]).toMatchObject({
      kind: 'actual',
      type: 'range',
      start: '2026-02-28T09:00:00Z',
      end: NOW.toISOString(),
      className: 'tl-actual tl-open tl-id-act-T1',
      subgroup: 'actual',
      content: 'First',
      title: 'T1 open: created 2026-02-28T09:00:00Z, still open',
    });
  });

  it('ends the actual span of a closed task where it closed', () => {
    const t = answer({ tasks: [{ ...answer().tasks[0]!, status: 'closed', closed_at: '2026-03-05T00:00:00Z' }] });
    const actual = buildTimeline(t, NOW).items.find((i) => i.kind === 'actual')!;
    expect(actual).toMatchObject({ end: '2026-03-05T00:00:00Z', className: 'tl-actual tl-closed tl-id-act-T1', title: 'T1 closed: created 2026-02-28T09:00:00Z, closed 2026-03-05T00:00:00Z' });
  });

  it('never ends a span before it began', () => {
    const t = answer({ tasks: [{ ...answer().tasks[0]!, created_at: '2026-03-11T00:00:00Z', planned_start: undefined, planned_end: undefined }] });
    expect(buildTimeline(t, NOW).items[0]).toMatchObject({ start: '2026-03-11T00:00:00Z', end: '2026-03-11T00:00:00Z' });
  });

  it('has no planned bar without a planned time, and lets one planned end stand for both', () => {
    const base = answer().tasks[0]!;
    const none = buildTimeline(answer({ tasks: [{ ...base, planned_start: undefined, planned_end: undefined }] }), NOW);
    expect(none.items.map((i) => i.id)).toEqual(['act:T1']);
    const onlyEnd = buildTimeline(answer({ tasks: [{ ...base, planned_start: undefined }] }), NOW).items[0]!;
    expect(onlyEnd).toMatchObject({ start: '2026-03-12T00:00:00Z', end: '2026-03-12T00:00:00Z' });
    const onlyStart = buildTimeline(answer({ tasks: [{ ...base, planned_end: undefined }] }), NOW).items[0]!;
    expect(onlyStart).toMatchObject({ start: '2026-03-01T00:00:00Z', end: '2026-03-01T00:00:00Z' });
    const backwards = buildTimeline(answer({ tasks: [{ ...base, planned_start: '2026-03-12T00:00:00Z', planned_end: '2026-03-01T00:00:00Z' }] }), NOW).items[0]!;
    expect(backwards).toMatchObject({ start: '2026-03-12T00:00:00Z', end: '2026-03-12T00:00:00Z' });
  });
});

describe('lease bars', () => {
  const leases: Timeline['leases'] = [
    { ref: 'L1', task: 'T1', holder: 'ada', state: 'live', started_at: '2026-03-09T10:00:00Z', intent: 'write it' },
    { ref: 'L2', task: 'T1', holder: 'bob', state: 'ended', started_at: '2026-03-02T10:00:00Z', ended_at: '2026-03-03T10:00:00Z', intent: 'review' },
    { ref: 'L3', task: 'T1', holder: 'cy', state: 'expired', started_at: '2026-03-04T10:00:00Z', ended_at: '2026-03-04T12:00:00Z', intent: 'lost' },
  ];

  it('draws one stacked sub-bar per lease, coloured by its state, with text in the tooltip', () => {
    const items = buildTimeline(answer({ leases }), NOW).items.filter((i) => i.kind === 'lease');
    expect(items.map((i) => [i.id, i.className, i.subgroup])).toEqual([
      ['lease:L1', 'tl-lease tl-lease-live', 'lease'],
      ['lease:L2', 'tl-lease tl-lease-ended', 'lease'],
      ['lease:L3', 'tl-lease tl-lease-expired', 'lease'],
    ]);
    expect(items[0]).toMatchObject({ group: 't:T1', type: 'range', start: '2026-03-09T10:00:00Z', end: NOW.toISOString(), content: 'ada', lease: 'L1', task: 'T1' });
    expect(items[0]!.title).toBe('L1 (live) held by ada: write it. Started 2026-03-09T10:00:00Z');
    expect(items[1]).toMatchObject({ end: '2026-03-03T10:00:00Z', title: 'L2 (ended) held by bob: review. Started 2026-03-02T10:00:00Z, ended 2026-03-03T10:00:00Z' });
    expect(items[2]).toMatchObject({ end: '2026-03-04T12:00:00Z', title: 'L3 (expired) held by cy: lost. Started 2026-03-04T10:00:00Z, expired 2026-03-04T12:00:00Z' });
  });

  it('writes the tooltip text from the lease alone and escapes it', () => {
    expect(leaseText({ ...leases[1]!, intent: '<i>' })).toContain('<i>');
    const items = buildTimeline(answer({ leases: [{ ...leases[0]!, intent: '<i>' }] }), NOW).items;
    expect(items.find((i) => i.kind === 'lease')!.title).toContain('&lt;i&gt;');
  });

  it('keeps a live lease that began after now as a zero-length bar', () => {
    const lease = { ...leases[0]!, started_at: '2026-03-11T00:00:00Z' };
    expect(buildTimeline(answer({ leases: [lease] }), NOW).items.find((i) => i.kind === 'lease')!.end).toBe('2026-03-11T00:00:00Z');
  });

  it('leaves out the leases of a task the answer does not hold', () => {
    const items = buildTimeline(answer({ leases: [{ ...leases[0]!, task: 'T9' }] }), NOW).items;
    expect(items.some((i) => i.kind === 'lease')).toBe(false);
  });
});

describe('markers', () => {
  const types = ['milestone', 'ask', 'check', 'done', 'closed'] as const;

  it('draws each of the five marker types as a point with its own class, glyph and tooltip', () => {
    const markers = types.map((type, n) => ({ task: 'T1', type, at: `2026-03-0${n + 1}T00:00:00Z`, label: `the ${type}` }));
    const items = buildTimeline(answer({ markers }), NOW).items.filter((i) => i.kind === 'marker');
    expect(items).toHaveLength(5);
    types.forEach((type, n) => {
      expect(items[n]).not.toHaveProperty('end');
      expect(items[n]).toMatchObject({
        type: 'point',
        group: 't:T1',
        subgroup: 'marker',
        start: `2026-03-0${n + 1}T00:00:00Z`,
        className: `tl-marker tl-marker-${type}`,
        content: MARKER_GLYPHS[type],
        title: `${type}: the ${type} (2026-03-0${n + 1}T00:00:00Z)`,
        task: 'T1',
      });
    });
    expect(MARKER_GLYPHS).toEqual({ milestone: '◆', ask: '?', check: '✓', done: '●', closed: '■' });
    expect(new Set(items.map((i) => i.id)).size).toBe(5);
  });

  it('gives two markers of one type at one time different ids, and leaves out those of an unheld task', () => {
    const at = '2026-03-02T00:00:00Z';
    const markers = [
      { task: 'T1', type: 'ask' as const, at, label: 'a' },
      { task: 'T1', type: 'ask' as const, at, label: 'b' },
      { task: 'T7', type: 'ask' as const, at, label: 'c' },
    ];
    const items = buildTimeline(answer({ markers }), NOW).items.filter((i) => i.kind === 'marker');
    expect(items.map((i) => i.id)).toEqual([`marker:T1:ask:${at}:0`, `marker:T1:ask:${at}:1`]);
  });
});

describe('dependency pairs', () => {
  it('pairs the blocking task with the blocked one, by their actual-span items, and drops a blocker outside the window', () => {
    const base = answer().tasks[0]!;
    const t = answer({
      tasks: [
        { ...base, ref: 'T1' },
        { ...base, ref: 'T2', deps: ['T1', 'T9'] },
        { ...base, ref: 'T3', deps: ['T1', 'T2'] },
      ],
    });
    expect(buildTimeline(t, NOW).dependencies).toEqual([
      { id: 'dep:T1>T2', from: 'T1', fromItem: 'act:T1', to: 'T2', toItem: 'act:T2' },
      { id: 'dep:T1>T3', from: 'T1', fromItem: 'act:T1', to: 'T3', toItem: 'act:T3' },
      { id: 'dep:T2>T3', from: 'T2', fromItem: 'act:T2', to: 'T3', toItem: 'act:T3' },
    ]);
    expect(buildTimeline(answer(), NOW).dependencies).toEqual([]);
  });
});

describe('the attention label on the task row', () => {
  it('shows the R2 badge texts on the row label and marks the row', () => {
    const t = answer({
      tasks: [
        {
          ...answer().tasks[0]!,
          attention: [{ reason: 'stranded' }, { reason: 'asking', blocking: true, asker_waiting: true }, { reason: 'done' }],
        },
      ],
    });
    const row = buildTimeline(t, NOW).groups[1]!;
    expect(row.attentionLabel).toBe('stranded, waiting on you · blocking · agent waiting, done, not closed');
    expect(row.content).toBe('T1 First <span class="tl-attention">stranded, waiting on you · blocking · agent waiting, done, not closed</span>');
    expect(row.className).toBe('tl-task tl-needs-attention');
    expect(row.title).toBe('T1 First (needs attention: stranded, waiting on you · blocking · agent waiting, done, not closed)');
  });

  it('has no label for a task that needs nothing', () => {
    const row = buildTimeline(answer(), NOW).groups[1]!;
    expect(row.attentionLabel).toBe('');
    expect(row.content).not.toContain('tl-attention');
  });
});

describe('opening a task', () => {
  it('names the task of a task row and none for an initiative row', () => {
    expect(taskOfGroup('t:T12')).toBe('T12');
    expect(taskOfGroup('i:alpha')).toBeUndefined();
  });
});

describe('filter round trip', () => {
  const full: TimelineFilter = { initiatives: ['alpha', 'beta'], tags: ['work', 'home'], actor: 'ada', closedWithin: 30 };

  it('encodes the default as an empty query and decodes it back', () => {
    expect(filterToQuery(DEFAULT_TIMELINE_FILTER)).toBe('');
    expect(filterFromQuery('')).toEqual(DEFAULT_TIMELINE_FILTER);
    expect(timelineHash(DEFAULT_TIMELINE_FILTER)).toBe('#/timeline');
    expect(isFiltered(DEFAULT_TIMELINE_FILTER)).toBe(false);
  });

  it('round-trips every field through the query and the fragment', () => {
    const query = filterToQuery(full);
    expect(query).toBe('initiative=alpha&initiative=beta&tag=work&tag=home&actor=ada&closed=30');
    expect(filterFromQuery(query)).toEqual(full);
    expect(timelineHash(full)).toBe(`#/timeline?${query}`);
    expect(filterFromHash(timelineHash(full))).toEqual(full);
    expect(filterFromHash('#/timeline')).toEqual(DEFAULT_TIMELINE_FILTER);
    expect(isFiltered(full)).toBe(true);
  });

  it('round-trips each closed-within option, and reads an unknown one as off', () => {
    for (const { value } of CLOSED_WITHIN_OPTIONS) {
      expect(filterFromQuery(filterToQuery({ ...DEFAULT_TIMELINE_FILTER, closedWithin: value })).closedWithin).toBe(value);
    }
    expect(CLOSED_WITHIN_OPTIONS.map((o) => `${o.value}:${o.label}`)).toEqual(['0:Off', '7:7 days', '30:30 days', '90:90 days']);
    expect(filterFromQuery('closed=45').closedWithin).toBe(0);
    expect(filterFromQuery('closed=').closedWithin).toBe(0);
    expect(filterToQuery({ ...DEFAULT_TIMELINE_FILTER, closedWithin: 0 })).toBe('');
  });

  it('round-trips values that need escaping', () => {
    const odd: TimelineFilter = { initiatives: ['a b'], tags: ['x&y=z'], actor: 'é ü', closedWithin: 7 };
    expect(filterFromQuery(filterToQuery(odd))).toEqual(odd);
  });

  it('drops blanks and repeats and trims the actor', () => {
    expect(filterFromQuery('initiative=a&initiative=a&initiative=%20&tag=&actor=%20ada%20')).toEqual({ initiatives: ['a'], tags: [], actor: 'ada', closedWithin: 0 });
    expect(filterToQuery({ initiatives: [' a ', 'a', ''], tags: [], actor: '  ', closedWithin: 0 })).toBe('initiative=a');
    expect(filterToQuery({ ...DEFAULT_TIMELINE_FILTER, actor: ' ada ' })).toBe('actor=ada');
  });

  it('builds the query of GET /timeline from the filter and the window', () => {
    const from = new Date('2026-02-24T12:00:00Z');
    const to = new Date('2026-03-17T12:00:00Z');
    expect(apiQuery(DEFAULT_TIMELINE_FILTER, from, to).toString()).toBe('from=2026-02-24T12%3A00%3A00.000Z&to=2026-03-17T12%3A00%3A00.000Z');
    const params = apiQuery(full, from, to);
    expect(params.getAll('initiative')).toEqual(['alpha', 'beta']);
    expect(params.getAll('tag')).toEqual(['work', 'home']);
    expect(params.get('actor')).toBe('ada');
    expect(params.get('closed_within')).toBe('30d');
    expect(params.get('from')).toBe('2026-02-24T12:00:00.000Z');
    expect(params.get('to')).toBe('2026-03-17T12:00:00.000Z');
  });
});

describe('itemClass', () => {
  it('names an item in the DOM by its id, with a character the CSS can select by', () => {
    expect(itemClass('act:alpha-1')).toBe('tl-id-act-alpha-1');
    expect(itemClass('lease:L3')).toBe('tl-id-lease-L3');
    expect(itemClass('a b.c')).toBe('tl-id-a-b-c');
  });

  it('is on the actual bar of every task, so an arrow finds it', () => {
    const task = answer().tasks[0]!;
    const bar = buildTimeline(answer(), NOW).items.find((i) => i.kind === 'actual')!;
    expect(bar.className.split(' ')).toContain(itemClass(actualItemId(task.ref)));
  });
});
