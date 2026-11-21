import { describe, expect, it } from 'vitest';
import {
  CLOSED_WITHIN_OPTIONS,
  DEFAULT_TIMELINE_FILTER,
  MARKER_GLYPHS,
  TIMELINE_LEGEND,
  INITIATIVE_ROW_PX,
  LANES,
  TASK_ROW_PX,
  laneStyle,
  affectsTimeline,
  choiceOptions,
  tagChoices,
  withClosedWithin,
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
  assignLeaseSlots,
  leaseSlotBox,
  LEASE_SLOTS,
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
        tags: [],
        tag_marks: [],
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
    expect(groups.map((g) => g.nestedInGroup)).toEqual([undefined, 'i:alpha', 'i:alpha', undefined, 'i:beta']);
    expect(groups[0]).toMatchObject({ nestedGroups: ['t:T1', 't:T3'], showNested: true, content: 'Alpha', title: 'alpha (work)', ref: 'alpha' });
    expect(groups[3]!.className).toBe('tl-initiative tl-initiative-closed');
    expect(groups[0]!.className).toBe('tl-initiative');
    expect(groups[3]!.title).toBe('beta');
    expect(groups[1]).toMatchObject({ content: 'T1 First', className: 'tl-task', title: 'T1 First', attentionLabel: '', ref: 'T1' });
    expect(Object.keys(groups[1]!).filter((k) => k.startsWith('subgroup'))).toEqual([]);
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
      task: 'T1',
    });
    expect(items[1]).toMatchObject({
      kind: 'actual',
      type: 'range',
      start: '2026-02-28T09:00:00Z',
      end: NOW.toISOString(),
      className: 'tl-actual tl-open tl-id-act-T1',
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

  it('draws one bar per lease in the lease lane, coloured by its state, with text in the tooltip', () => {
    const items = buildTimeline(answer({ leases }), NOW).items.filter((i) => i.kind === 'lease');
    expect(items.map((i) => [i.id, i.className, i.kind])).toEqual([
      ['lease:L1', 'tl-lease tl-lease-live tl-lease-c1 tl-lease-c1-s0', 'lease'],
      ['lease:L2', 'tl-lease tl-lease-ended tl-lease-c1 tl-lease-c1-s0', 'lease'],
      ['lease:L3', 'tl-lease tl-lease-expired tl-lease-c1 tl-lease-c1-s0', 'lease'],
    ]);
    expect(items[0]).toMatchObject({ group: 't:T1', type: 'range', start: '2026-03-09T10:00:00Z', end: NOW.toISOString(), content: 'ada', lease: 'L1', task: 'T1' });
    expect(items[0]!.title).toBe('L1 (live) held by ada: write it. Started 2026-03-09T10:00:00Z');
    expect(items[1]).toMatchObject({ end: '2026-03-03T10:00:00Z', title: 'L2 (ended) held by bob: review. Started 2026-03-02T10:00:00Z, ended 2026-03-03T10:00:00Z' });
    expect(items[2]).toMatchObject({ end: '2026-03-04T12:00:00Z', title: 'L3 (expired) held by cy: lost. Started 2026-03-04T10:00:00Z, expired 2026-03-04T12:00:00Z' });
  });

  it('puts no item in a subgroup, so vis-timeline never sizes a row by the items in view', () => {
    const items = buildTimeline(answer({ leases }), NOW).items;
    expect(items.length).toBeGreaterThan(0);
    for (const item of items) expect(item).not.toHaveProperty('subgroup');
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

describe('the tag chips on the task row', () => {
  type Mark = { tag: string; source: 'task' | 'initiative' | 'actor' };
  const marks: Mark[] = [{ tag: 'urgent', source: 'task' }, { tag: 'work', source: 'initiative' }, { tag: 'work', source: 'task' }];
  const marked = (tag_marks: Mark[] = marks) => answer({ tasks: [{ ...answer().tasks[0]!, tags: ['urgent', 'work'], tag_marks }] });

  it('gives the row each tag once with every source it has, in the order the task’s own marks name them, and marks the row', () => {
    const row = buildTimeline(marked(), NOW).groups[1]!;
    expect(row.tags).toEqual([
      { tag: 'urgent', sources: ['task'] },
      { tag: 'work', sources: ['initiative', 'task'] },
    ]);
    expect(row.className).toBe('tl-task tl-tagged');
    expect(row.text).toBe('T1 First');
    expect(row.title).toBe('T1 First tags: urgent, work');
  });

  it('draws a derived actor-kind tag and an initiative tag as a chip with its source though the task has no tag of its own', () => {
    const row = buildTimeline(marked([{ tag: 'web', source: 'initiative' }, { tag: 'claude', source: 'actor' }]), NOW).groups[1]!;
    expect(row.tags).toEqual([
      { tag: 'web', sources: ['initiative'] },
      { tag: 'claude', sources: ['actor'] },
    ]);
    expect(row.className).toBe('tl-task tl-tagged');
  });

  it('draws each row from its own task’s marks, not another task’s', () => {
    const base = answer().tasks[0]!;
    const two = answer({ tasks: [{ ...base, tag_marks: [{ tag: 'one', source: 'task' }] }, { ...base, ref: 'T2', tag_marks: [{ tag: 'two', source: 'actor' }] }] });
    const rows = buildTimeline(two, NOW).groups.filter((g) => g.ref === 'T1' || g.ref === 'T2');
    expect(rows.map((g) => g.tags)).toEqual([[{ tag: 'one', sources: ['task'] }], [{ tag: 'two', sources: ['actor'] }]]);
  });

  it('keeps the attention badge in the label and the tooltip', () => {
    const t = answer({ tasks: [{ ...marked().tasks[0]!, attention: [{ reason: 'done' }] }] });
    const row = buildTimeline(t, NOW).groups[1]!;
    expect(row.className).toBe('tl-task tl-needs-attention tl-tagged');
    expect(row.attentionLabel).toBe('done, not closed');
    expect(row.title).toBe('T1 First (needs attention: done, not closed) tags: urgent, work');
  });

  it('leaves the row as it was for a task with no marks', () => {
    const row = buildTimeline(answer(), NOW).groups[1]!;
    expect(row.content).toBe('T1 First');
    expect(row.tags).toEqual([]);
    expect(row.className).toBe('tl-task');
    expect(row.title).toBe('T1 First');
  });

  it('gives an initiative row no tags', () => {
    expect(buildTimeline(marked(), NOW).groups[0]!.tags).toEqual([]);
  });
});

describe('opening a task', () => {
  it('names the task of a task row and none for an initiative row', () => {
    expect(taskOfGroup('t:T12')).toBe('T12');
    expect(taskOfGroup('i:alpha')).toBeUndefined();
  });
});

describe('filter round trip', () => {
  const full: TimelineFilter = { initiatives: ['alpha', 'beta'], tags: ['work', 'home'], actor: 'ada', closedWithin: 30, activeOnly: false, order: 'default' as const };

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

  it('keeps Only active as active=1 and Active first as the shared sort=active&dir=desc, both in the fragment', () => {
    const both: TimelineFilter = { ...DEFAULT_TIMELINE_FILTER, activeOnly: true, order: 'active' };
    expect(filterToQuery(both)).toBe('active=1&sort=active&dir=desc');
    expect(filterFromQuery('active=1&sort=active&dir=desc')).toEqual(both);
    expect(timelineHash(both)).toBe('#/timeline?active=1&sort=active&dir=desc');
    expect(filterToQuery({ ...DEFAULT_TIMELINE_FILTER, order: 'active' })).toBe('sort=active&dir=desc');
    expect(isFiltered({ ...DEFAULT_TIMELINE_FILTER, activeOnly: true })).toBe(true);
    expect(filterFromQuery('active=yes&sort=ref&dir=desc')).toEqual(DEFAULT_TIMELINE_FILTER);
    expect(filterFromQuery('sort=active').order).toBe('active');
  });

  it('reads and sends each tag in the server\'s spelling, so an upper-case or padded saved tag is the chip the rows carry', () => {
    const filter = filterFromQuery('tag=%20OWN-TAG%20&tag=CODER&tag=own-tag');
    expect(filter.tags).toEqual(['own-tag', 'coder']);
    expect(apiQuery({ ...DEFAULT_TIMELINE_FILTER, tags: [' Work ', 'work'] }, new Date(0), new Date(1)).getAll('tag')).toEqual(['work']);
  });

  it('sends active=true and the window to GET /timeline only when asked', () => {
    const w = [new Date('2026-03-01T00:00:00Z'), new Date('2026-03-02T00:00:00Z')] as const;
    expect(apiQuery(DEFAULT_TIMELINE_FILTER, ...w).has('active')).toBe(false);
    expect(apiQuery(DEFAULT_TIMELINE_FILTER, ...w).has('window')).toBe(false);
    const asked = apiQuery({ ...DEFAULT_TIMELINE_FILTER, activeOnly: true }, ...w, '8h');
    expect([asked.get('active'), asked.get('window'), asked.get('rows')]).toEqual(['true', '8h', 'all']);
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
    const odd: TimelineFilter = { initiatives: ['a b'], tags: ['x&y=z'], actor: 'é ü', closedWithin: 7, activeOnly: false, order: 'default' as const };
    expect(filterFromQuery(filterToQuery(odd))).toEqual(odd);
  });

  it('drops blanks and repeats and trims the actor', () => {
    expect(filterFromQuery('initiative=a&initiative=a&initiative=%20&tag=&actor=%20ada%20')).toEqual({ initiatives: ['a'], tags: [], actor: 'ada', closedWithin: 0, activeOnly: false, order: 'default' as const });
    expect(filterToQuery({ initiatives: [' a ', 'a', ''], tags: [], actor: '  ', closedWithin: 0, activeOnly: false, order: 'default' as const })).toBe('initiative=a');
    expect(filterToQuery({ ...DEFAULT_TIMELINE_FILTER, actor: ' ada ' })).toBe('actor=ada');
  });

  it('builds the query of GET /timeline from the filter and the window', () => {
    const from = new Date('2026-02-24T12:00:00Z');
    const to = new Date('2026-03-17T12:00:00Z');
    expect(apiQuery(DEFAULT_TIMELINE_FILTER, from, to).toString()).toBe('from=2026-02-24T12%3A00%3A00.000Z&to=2026-03-17T12%3A00%3A00.000Z&rows=all');
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

describe('the filter controls', () => {
  it('offers "any" first, then the values sorted and once, and keeps a current value the list lacks', () => {
    expect(choiceOptions(['beta', 'alpha', 'beta', ' '], '', 'Any tag')).toEqual([
      { value: '', label: 'Any tag' },
      { value: 'alpha', label: 'alpha' },
      { value: 'beta', label: 'beta' },
    ]);
    expect(choiceOptions(['beta'], 'zed', 'Any tag').map((o) => o.value)).toEqual(['', 'beta', 'zed']);
    expect(choiceOptions([], '', 'Any actor')).toEqual([{ value: '', label: 'Any actor' }]);
  });

  it('offers the tags of the initiatives as chips, each once, sorted, marked as the initiative\'s, and keeps a chosen one the board lacks', () => {
    expect(tagChoices([{ tags: ['work', 'home'] }, { tags: ['work'] }, { tags: [] }], [])).toEqual([
      { tag: 'home', sources: ['initiative'] },
      { tag: 'work', sources: ['initiative'] },
    ]);
    expect(tagChoices([{ tags: ['work'] }], ['zed'])).toEqual([
      { tag: 'work', sources: ['initiative'] },
      { tag: 'zed', sources: [] },
    ]);
    expect(tagChoices([], [])).toEqual([]);
  });

  it('offers the tags of the task rows drawn with their sources, and keeps the chips offered before', () => {
    const rows = [{ tags: [{ tag: 'review-own', sources: ['task' as const] }, { tag: 'ralph', sources: ['actor' as const] }] }, { tags: [{ tag: 'work', sources: ['task' as const] }] }];
    expect(tagChoices([{ tags: ['work'] }], [], rows)).toEqual([
      { tag: 'ralph', sources: ['actor'] },
      { tag: 'review-own', sources: ['task'] },
      { tag: 'work', sources: ['task', 'initiative'] },
    ]);
    const seen = tagChoices([], [], rows);
    expect(tagChoices([], [], [], seen)).toEqual(seen);
  });

  it('reads the closed-within select, an unknown value is off', () => {
    expect(withClosedWithin(DEFAULT_TIMELINE_FILTER, '30').closedWithin).toBe(30);
    expect(withClosedWithin({ ...DEFAULT_TIMELINE_FILTER, closedWithin: 7 }, '0').closedWithin).toBe(0);
    expect(withClosedWithin({ ...DEFAULT_TIMELINE_FILTER, closedWithin: 7 }, 'x').closedWithin).toBe(0);
  });

  it('refetches on lease, task, ask and milestone events, a check and a dependency, and on nothing else', () => {
    for (const type of ['lease_started', 'lease_renewed', 'lease_ended', 'lease_expired', 'task_created', 'task_done', 'task_closed', 'ask_raised', 'ask_answered', 'milestone', 'milestone_added', 'check_recorded', 'dep_added', 'dep_removed']) {
      expect(affectsTimeline({ type }), type).toBe(true);
    }
    for (const type of ['note_added', 'initiative_created', 'key_created', 'notification_sent', 'xlease_started', '']) {
      expect(affectsTimeline({ type }), type).toBe(false);
    }
  });
});

describe('item classes and TIMELINE_LEGEND', () => {
  const base = answer().tasks[0]!;
  const tasks = (['open', 'done', 'closed'] as const).map((status, n) => ({
    ...base,
    ref: `T${n + 1}`,
    status,
    closed_at: status === 'open' ? undefined : '2026-03-05T00:00:00Z',
  }));
  const leases = (['live', 'expired', 'ended'] as const).map((state, n) => ({
    ref: `L${n + 1}`,
    task: 'T1',
    holder: 'ada',
    state,
    started_at: '2026-03-02T10:00:00Z',
    ended_at: state === 'live' ? undefined : '2026-03-03T10:00:00Z',
    intent: 'x',
  }));
  const markers = (['milestone', 'ask', 'check', 'done', 'closed'] as const).map((type, n) => ({ task: 'T1', type, at: `2026-03-0${n + 1}T00:00:00Z`, label: type }));
  const classesOf = (kind: string) =>
    buildTimeline(answer({ tasks, leases, markers }), NOW)
      .items.filter((i) => i.kind === kind)
      .map((i) => i.className.split(' ').filter((c) => !c.startsWith('tl-id-') && !/^tl-lease-c\d/.test(c)).join(' '));

  it('gives each task status, lease state and marker type its class, and a planned bar its own', () => {
    expect(classesOf('actual')).toEqual(['tl-actual tl-open', 'tl-actual tl-done', 'tl-actual tl-closed']);
    expect(classesOf('planned')).toEqual(['tl-planned', 'tl-planned', 'tl-planned']);
    expect(classesOf('lease')).toEqual(['tl-lease tl-lease-live', 'tl-lease tl-lease-expired', 'tl-lease tl-lease-ended']);
    expect(classesOf('marker')).toEqual(['milestone', 'ask', 'check', 'done', 'closed'].map((t) => `tl-marker tl-marker-${t}`));
  });

  it('holds exactly the classes the chart draws, markers by their glyph, each with its own label', () => {
    expect(TIMELINE_LEGEND.map((e) => e.className)).toEqual([
      'tl-planned',
      'tl-actual tl-open',
      'tl-actual tl-done',
      'tl-actual tl-closed',
      'tl-lease tl-lease-live',
      'tl-lease tl-lease-expired',
      'tl-lease tl-lease-ended',
      'tl-marker tl-marker-milestone',
      'tl-marker tl-marker-ask',
      'tl-marker tl-marker-check',
      'tl-marker tl-marker-done',
      'tl-marker tl-marker-closed',
    ]);
    const drawn = new Set(['planned', 'actual', 'lease', 'marker'].flatMap(classesOf));
    expect(new Set(TIMELINE_LEGEND.map((e) => e.className))).toEqual(drawn);
    for (const [type, glyph] of Object.entries(MARKER_GLYPHS)) {
      expect(TIMELINE_LEGEND.find((e) => e.className === `tl-marker tl-marker-${type}`)?.glyph).toBe(glyph);
    }
    expect(TIMELINE_LEGEND.filter((e) => e.glyph !== undefined)).toHaveLength(5);
    expect(new Set(TIMELINE_LEGEND.map((e) => e.label)).size).toBe(TIMELINE_LEGEND.length);
  });
});

describe('fixed rows and lanes', () => {
  it('fixes a task row at 52 px and an initiative row at 44 px', () => {
    expect(TASK_ROW_PX).toBe(52);
    expect(INITIATIVE_ROW_PX).toBe(44);
    expect(laneStyle()).toContain('--tl-task-row:52px');
    expect(laneStyle()).toContain('--tl-initiative-row:44px');
  });

  it('puts the planned strip on top, the actual span (with the markers) below it and the leases below that', () => {
    const { planned, actual, marker, lease } = LANES;
    expect(planned.top + planned.height).toBeLessThanOrEqual(actual.top);
    expect(actual.top + actual.height).toBeLessThanOrEqual(lease.top);
    expect(marker).toEqual(actual);
  });

  it('keeps every lane inside the task row and none overlapping another', () => {
    for (const lane of Object.values(LANES)) {
      expect(lane.top).toBeGreaterThanOrEqual(0);
      expect(lane.top + lane.height).toBeLessThanOrEqual(TASK_ROW_PX);
    }
  });

  it('hands every lane to the CSS as a top offset and a height', () => {
    const style = laneStyle();
    for (const [kind, lane] of Object.entries(LANES)) {
      expect(style).toContain(`--tl-${kind}-top:${lane.top}px`);
      expect(style).toContain(`--tl-${kind}-h:${lane.height}px`);
    }
  });

  it('draws every lease and marker of one task on a lane, never in a subgroup that stacks', () => {
    const items = buildTimeline(
      answer({
        leases: [
          { ref: 'L1', task: 'T1', holder: 'a', state: 'live', started_at: '2026-03-01T00:00:00Z', intent: '' },
          { ref: 'L2', task: 'T1', holder: 'b', state: 'live', started_at: '2026-03-02T00:00:00Z', intent: '' },
        ],
      }),
      NOW,
    ).items;
    const leaseItems = items.filter((i) => i.kind === 'lease');
    expect(leaseItems).toHaveLength(2);
    expect(new Set(leaseItems.map((i) => i.group))).toEqual(new Set(['t:T1']));
  });
});

describe('lease slots (R11)', () => {
  const at = (day: number) => Date.UTC(2026, 2, day);
  const span = (id: string, from: number, to: number) => ({ id, start: at(from), end: at(to) });

  /** Whether every bar of the slotted spans shows a strip of its box that no other bar's box covers. */
  function everyBarHasAnUncoveredStrip(slots: Map<string, number>, count: number): boolean {
    const boxes = [...slots.values()].map((slot) => leaseSlotBox(slot, count));
    const rowsOf = (box: { top: number; height: number }) => Array.from({ length: box.height }, (_, n) => box.top + n);
    return boxes.every((box, i) => {
      const others = new Set(boxes.filter((_, j) => j !== i).flatMap(rowsOf));
      return rowsOf(box).some((row) => !others.has(row));
    });
  }

  it('puts two overlapping leases in two slots, the later one below, each bar with a strip of its own', () => {
    const { slots, count } = assignLeaseSlots([span('a', 1, 5), span('b', 3, 8)]);
    expect([...slots]).toEqual([['a', 0], ['b', 1]]);
    expect(count).toBe(2);
    expect(leaseSlotBox(0, count)).toEqual({ top: 34, height: 11 });
    expect(leaseSlotBox(1, count)).toEqual({ top: 39, height: 11 });
    expect(everyBarHasAnUncoveredStrip(slots, count)).toBe(true);
  });

  it('puts three leases, one of them inside another, in three slots that every bar shows a strip of', () => {
    const { slots, count } = assignLeaseSlots([span('outer', 1, 10), span('inner', 3, 6), span('late', 5, 12)]);
    expect([...slots].sort()).toEqual([['inner', 1], ['late', 2], ['outer', 0]]);
    expect(count).toBe(LEASE_SLOTS);
    expect([0, 1, 2].map((slot) => leaseSlotBox(slot, count))).toEqual([
      { top: 34, height: 6 },
      { top: 39, height: 6 },
      { top: 44, height: 6 },
    ]);
    expect(everyBarHasAnUncoveredStrip(slots, count)).toBe(true);
    for (const slot of [0, 1, 2]) expect(leaseSlotBox(slot, count).top + leaseSlotBox(slot, count).height).toBeLessThanOrEqual(TASK_ROW_PX);
  });

  it('lets two leases that touch end to start share one slot, at the full lane', () => {
    const { slots, count } = assignLeaseSlots([span('second', 5, 9), span('first', 1, 5)]);
    expect([...slots].sort()).toEqual([['first', 0], ['second', 0]]);
    expect(count).toBe(1);
    expect(leaseSlotBox(0, count)).toEqual(LANES.lease);
  });
});
