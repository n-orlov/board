// The Timeline view's model (PRD R6): turns a `GET /timeline` answer into the groups and items vis-timeline draws, and
// encodes the view's filters in the URL fragment's query (`#/timeline?initiative=aa&tag=work&actor=ada&closed=30`) and
// in the query of `GET /timeline`.
//
// Groups are initiatives (collapsible, holding their tasks as `nestedGroups`) and tasks. A task row carries a light
// planned bar, the actual span (created, to closed or now), one bar per lease coloured by state, and markers as points.
// Each kind keeps a lane of its own at a fixed place in a row of fixed height (Timeline.svelte), never stacked, so a
// row is as tall wherever the chart is scrolled to. Dependencies come out as pairs of actual-span item ids for an arrow drawer.
// Text that vis-timeline renders as HTML (`content`, `title`) is escaped here.
import type { components } from './api/schema';
import { attentionBadges, clean } from './attention';
import { mergeChoices, type TagChoice, type TagMark } from './chipFilter';
import { ACTIVE_COLUMNS, type TimelineOrder } from './timelineActive';
import { sortFragment, sortFromFragment } from './sortControl';
import type { NowWindow } from './nowFlight';
import { tagChips } from './taskTags';

export type Timeline = components['schemas']['Timeline'];
/**
 * An answer as the view draws it. With `activeFirst` (the refs of its active tasks, Active first on the whole board)
 * the rows are drawn active first: every active task above every idle one (R8, `buildTimeline`).
 */
export type TimelineAnswer = Timeline & {
  activeFirst?: readonly string[];
};
type TimelineTask = components['schemas']['TimelineTask'];
type TimelineLease = components['schemas']['TimelineLease'];
type TimelineMarker = components['schemas']['TimelineMarker'];

export type LeaseState = TimelineLease['state'];
export type MarkerType = TimelineMarker['type'];
export type ItemKind = 'planned' | 'actual' | 'lease' | 'marker';

/** A row of the timeline: an initiative (with `nestedGroups`) or a task. */
export interface TimelineGroup {
  id: string;
  /** The row label, as HTML (escaped text, plus the attention badge's span). */
  content: string;
  /** Position among all groups: initiatives in answer order, each followed by its tasks (Active first: `buildTimeline`). */
  order: number;
  className: string;
  /** The tooltip of the row label. */
  title: string;
  /**
   * The row's tags as chips, in the order the marks name them (empty for an initiative row or a task with none). vis-timeline
   * strips `class`, `style` and `data-` attributes from an HTML label, so `groupTemplate` (lib/timelineLabel.ts) builds the label's elements from this.
   */
  tags: TagChoice[];
  /** A task row's `ref title` as plain text, which the label with tags is built from. */
  text?: string;
  /** An initiative lists its task groups here; it can be collapsed. */
  nestedGroups?: string[];
  showNested?: boolean;
  /**
   * The initiative row a task row is drawn under. Given on every task row, so a task that moves to another initiative
   * row (Active first and back) is moved in vis-timeline too, which re-nests rows only when one is added.
   */
  nestedInGroup?: string;
  /** The ref of a task row, or the slug of an initiative row. */
  ref: string;
  /** The R2 attention label of a task row (the badges' texts joined); empty when the task needs nothing. */
  attentionLabel: string;
}

/** A bar or a point on a task row. */
export interface TimelineItem {
  id: string;
  group: string;
  kind: ItemKind;
  type: 'range' | 'point';
  /** The text drawn on the item, as HTML. */
  content: string;
  /** The tooltip, as HTML. */
  title: string;
  start: string;
  end?: string;
  className: string;
  /** The ref of the task the item is on (clicking opens its page). */
  task: string;
  /** The lease's ref, on a lease bar. */
  lease?: string;
}

/** An arrow from the blocking task's actual span to the blocked task's. */
export interface DependencyPair {
  id: string;
  /** The blocking task and its actual-span item. */
  from: string;
  fromItem: string;
  /** The blocked task and its actual-span item. */
  to: string;
  toItem: string;
}

export interface TimelineModel {
  groups: TimelineGroup[];
  items: TimelineItem[];
  dependencies: DependencyPair[];
}

export const MARKER_GLYPHS: Readonly<Record<MarkerType, string>> = {
  milestone: '◆',
  ask: '?',
  check: '✓',
  done: '●',
  closed: '■',
};

/** One key of the Timeline's legend: the item classes it shows (styled like the chart's items), a glyph for a marker. */
export interface LegendEntry {
  className: string;
  label: string;
  glyph?: string;
}

/**
 * The legend, in the order it reads. The colours are the view's CSS (Timeline.svelte), on the design tokens: a task bar
 * open is accent, done success, closed muted, planned a dashed muted outline; a lease live is success, expired warning
 * (the same amber as a stranded task on Attention), ended muted; a milestone is accent, an ask warning, a check and done
 * success, a close muted.
 */
export const TIMELINE_LEGEND: readonly LegendEntry[] = [
  { className: 'tl-planned', label: 'Planned' },
  { className: 'tl-actual tl-open', label: 'Open' },
  { className: 'tl-actual tl-done', label: 'Done' },
  { className: 'tl-actual tl-closed', label: 'Closed' },
  { className: 'tl-lease tl-lease-live', label: 'Lease live' },
  { className: 'tl-lease tl-lease-expired', label: 'Lease expired' },
  { className: 'tl-lease tl-lease-ended', label: 'Lease ended' },
  { className: 'tl-marker tl-marker-milestone', label: 'Milestone', glyph: MARKER_GLYPHS.milestone },
  { className: 'tl-marker tl-marker-ask', label: 'Ask', glyph: MARKER_GLYPHS.ask },
  { className: 'tl-marker tl-marker-check', label: 'Check', glyph: MARKER_GLYPHS.check },
  { className: 'tl-marker tl-marker-done', label: 'Marked done', glyph: MARKER_GLYPHS.done },
  { className: 'tl-marker tl-marker-closed', label: 'Marked closed', glyph: MARKER_GLYPHS.closed },
];

const ESCAPES: Readonly<Record<string, string>> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };

/** Text made safe to hand vis-timeline, which draws `content` and `title` as HTML. */
export function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => ESCAPES[c]!);
}

export function initiativeGroupId(slug: string): string {
  return `i:${slug}`;
}

/**
 * The second row of an initiative under Active first: the one its idle tasks are drawn under, below every active task,
 * when its active tasks are drawn under its first row.
 */
export function idleGroupId(slug: string): string {
  return `i:${slug}:idle`;
}

export function taskGroupId(ref: string): string {
  return `t:${ref}`;
}

export function plannedItemId(ref: string): string {
  return `plan:${ref}`;
}

export function actualItemId(ref: string): string {
  return `act:${ref}`;
}

/** The height of a task row and of an initiative row, in px: fixed, so a row is as tall wherever the chart is scrolled to. */
export const TASK_ROW_PX = 52;
export const INITIATIVE_ROW_PX = 44;

/** A lane of a task row: the item's top offset in the row and its height, in px. */
export interface Lane {
  top: number;
  height: number;
}

/**
 * Each kind of item keeps a lane of its own in a task row, from the top down: the planned strip, the actual span (the
 * markers sit on its lane) and the leases (overlapping leases share theirs). Items are never stacked, so no lane moves.
 */
export const LANES: Record<'planned' | 'actual' | 'marker' | 'lease', Lane> = {
  planned: { top: 4, height: 6 },
  actual: { top: 12, height: 20 },
  marker: { top: 12, height: 20 },
  lease: { top: 34, height: 14 },
};

/** The lanes and row heights as the CSS custom properties Timeline.svelte's rules read. */
export function laneStyle(): string {
  const lanes = (Object.keys(LANES) as (keyof typeof LANES)[]).map(
    (k) => `--tl-${k}-top:${LANES[k].top}px;--tl-${k}-h:${LANES[k].height}px`,
  );
  const slots = [2, 3].flatMap((count) => [
    `--tl-lease-c${count}-h:${leaseSlotBox(0, count).height}px`,
    ...Array.from({ length: count }, (_, slot) => `--tl-lease-c${count}-s${slot}-top:${leaseSlotBox(slot, count).top}px`),
  ]);
  return [`--tl-task-row:${TASK_ROW_PX}px`, `--tl-initiative-row:${INITIATIVE_ROW_PX}px`, ...lanes, ...slots].join(';');
}

/**
 * The most slots a task's leases are spread over (R11). A lease takes the first slot whose last bar ended at or before
 * it began, so leases that touch end to start share one; a fourth lease overlapping three others shares the last slot.
 */
export const LEASE_SLOTS = 3;

/** The area the lease slots divide, below the actual span: 16 px from 34 px down, leaving the row's last 2 px free. */
const LEASE_AREA_PX = 16;

/** The bar height for a task whose leases need `count` slots; the slots step down by (area - height) / (count - 1), so
 * the bars overlap but each shows a strip no other bar covers (the middle one of three needs 2 x step > height). */
const LEASE_SLOT_HEIGHT: readonly number[] = [LANES.lease.height, 11, 6];

/** Where a lease bar sits in a row whose leases fill `count` (1 to LEASE_SLOTS) slots: its top offset in the row and its height, in px. */
export function leaseSlotBox(slot: number, count: number): Lane {
  const height = LEASE_SLOT_HEIGHT[count - 1]!;
  const step = count < 2 ? 0 : (LEASE_AREA_PX - height) / (count - 1);
  return { top: LANES.lease.top + slot * step, height };
}

/** The class that places a lease bar: `tl-lease-c<count>` sets its height, `tl-lease-c<count>-s<slot>` its top. */
export function leaseSlotClass(slot: number, count: number): string {
  return `tl-lease-c${count} tl-lease-c${count}-s${slot}`;
}

/** One lease's span, in ms since the epoch, for slot assignment. */
export interface LeaseSpan {
  id: string;
  start: number;
  end: number;
}

/**
 * The slot (0 is the top one) of each lease of ONE task: leases in the order they began each take the first slot free
 * at their start (a slot is free from the instant its last bar ended, so end-to-start leases share it), and no more than
 * `LEASE_SLOTS` slots exist. The count is how many slots the task's leases use.
 */
export function assignLeaseSlots(spans: readonly LeaseSpan[]): { slots: Map<string, number>; count: number } {
  const slots = new Map<string, number>();
  const freeAt: number[] = [];
  for (const span of [...spans].sort((a, b) => a.start - b.start)) {
    const free = freeAt.findIndex((end) => end <= span.start);
    const slot = free >= 0 ? free : Math.min(freeAt.length, LEASE_SLOTS - 1);
    freeAt[slot] = Math.max(span.end, freeAt[slot] ?? span.end);
    slots.set(span.id, slot);
  }
  return { slots, count: Math.max(1, freeAt.length) };
}

/** The CSS class that names an item in the DOM (vis-timeline draws no id attribute), for the dependency arrows. */
export function itemClass(itemId: string): string {
  return `tl-id-${itemId.replace(/[^A-Za-z0-9_-]/g, '-')}`;
}

export function leaseItemId(ref: string): string {
  return `lease:${ref}`;
}

function laterOf(a: string, b: string): string {
  return Date.parse(b) > Date.parse(a) ? b : a;
}

/** The end of a task's actual span: when it closed, or `now` while it is open (never before it began). */
function actualEnd(task: TimelineTask, nowIso: string): string {
  return task.closed_at ?? laterOf(task.created_at, nowIso);
}

function taskGroup(task: TimelineTask, order: number, parent: string, marks: readonly TagMark[]): TimelineGroup {
  const label = attentionBadges(task.attention)
    .map((b) => b.label)
    .join(', ');
  const text = `${task.ref} ${task.title}`;
  const tags = tagChips(marks);
  const named = [text, ...(label === '' ? [] : [`(needs attention: ${label})`]), ...(tags.length === 0 ? [] : [`tags: ${tags.map((c) => c.tag).join(', ')}`])];
  return {
    id: taskGroupId(task.ref),
    content: escapeHtml(text) + (label === '' ? '' : ` <span class="tl-attention">${escapeHtml(label)}</span>`),
    order,
    className: ['tl-task', ...(label === '' ? [] : ['tl-needs-attention']), ...(tags.length === 0 ? [] : ['tl-tagged'])].join(' '),
    title: escapeHtml(named.join(' ')),
    text,
    tags,
    ref: task.ref,
    attentionLabel: label,
    nestedInGroup: parent,
  };
}

function plannedItem(task: TimelineTask): TimelineItem | undefined {
  // One planned end alone stands for both (SPEC 6.10); a planned end before the start is drawn from the start.
  const start = task.planned_start ?? task.planned_end;
  if (start === undefined) return undefined;
  const end = laterOf(start, task.planned_end ?? start);
  return {
    id: plannedItemId(task.ref),
    group: taskGroupId(task.ref),
    kind: 'planned',
    type: 'range',
    content: '',
    title: escapeHtml(`${task.ref} planned ${start} to ${end}`),
    start,
    end,
    className: 'tl-planned',
    task: task.ref,
  };
}

function actualItem(task: TimelineTask, nowIso: string): TimelineItem {
  const end = actualEnd(task, nowIso);
  const closed = task.closed_at === undefined ? 'still open' : `closed ${task.closed_at}`;
  const id = actualItemId(task.ref);
  return {
    id,
    group: taskGroupId(task.ref),
    kind: 'actual',
    type: 'range',
    content: escapeHtml(task.title),
    title: escapeHtml(`${task.ref} ${task.status}: created ${task.created_at}, ${closed}`),
    start: task.created_at,
    end,
    className: `tl-actual tl-${task.status} ${itemClass(id)}`,
    task: task.ref,
  };
}

/** The tooltip text of a lease bar: its ref, state, holder, intent and times. */
export function leaseText(lease: TimelineLease): string {
  const end = lease.ended_at === undefined ? '' : `, ${lease.state} ${lease.ended_at}`;
  return `${lease.ref} (${lease.state}) held by ${lease.holder}: ${lease.intent}. Started ${lease.started_at}${end}`;
}

/** The end of a lease bar: when it ended, or `now` while it is live (never before it began). */
function leaseEnd(lease: TimelineLease, nowIso: string): string {
  return lease.ended_at ?? laterOf(lease.started_at, nowIso);
}

function leaseItem(lease: TimelineLease, nowIso: string, place: string): TimelineItem {
  return {
    id: leaseItemId(lease.ref),
    group: taskGroupId(lease.task),
    kind: 'lease',
    type: 'range',
    content: escapeHtml(lease.holder),
    title: escapeHtml(leaseText(lease)),
    start: lease.started_at,
    end: leaseEnd(lease, nowIso),
    className: `tl-lease tl-lease-${lease.state} ${place}`,
    task: lease.task,
    lease: lease.ref,
  };
}

function markerItem(marker: TimelineMarker, index: number): TimelineItem {
  return {
    id: `marker:${marker.task}:${marker.type}:${marker.at}:${index}`,
    group: taskGroupId(marker.task),
    kind: 'marker',
    type: 'point',
    content: MARKER_GLYPHS[marker.type],
    title: escapeHtml(`${marker.type}: ${marker.label} (${marker.at})`),
    start: marker.at,
    className: `tl-marker tl-marker-${marker.type}`,
    task: marker.task,
  };
}

/** The slugs of the initiatives the rows come under: the answer's own, then any a task names that the answer lacks. */
function initiativeSlugs(timeline: Timeline): string[] {
  const slugs = timeline.initiatives.map((i) => i.slug);
  for (const task of timeline.tasks) if (!slugs.includes(task.initiative)) slugs.push(task.initiative);
  return slugs;
}

/** One initiative row and the task rows drawn under it. */
interface Section {
  id: string;
  slug: string;
  own: TimelineTask[];
}

/** The initiative rows in answer order, each holding its tasks in answer order (the current order). */
function sectionsInOrder(timeline: Timeline, keep: (task: TimelineTask) => boolean, id: (slug: string) => string): Section[] {
  return initiativeSlugs(timeline).map((slug) => ({ id: id(slug), slug, own: timeline.tasks.filter((t) => t.initiative === slug && keep(t)) }));
}

/**
 * The rows top to bottom. In the current order: each initiative, then its tasks. Active first: the current order of
 * the active tasks (each under its initiative's row), then the current order of the idle ones, so read top to bottom
 * no idle task is above an active one. An initiative holding both is drawn twice: its active tasks under its row and
 * its idle tasks under a second row further down (`idleGroupId`); an initiative with no task stays among the idle.
 */
function sections(timeline: TimelineAnswer): Section[] {
  if (timeline.activeFirst === undefined) return sectionsInOrder(timeline, () => true, initiativeGroupId);
  const active = new Set(timeline.activeFirst);
  const lead = sectionsInOrder(timeline, (t) => active.has(t.ref), initiativeGroupId).filter((s) => s.own.length > 0);
  const led = new Set(lead.map((s) => s.slug));
  const rest = sectionsInOrder(timeline, (t) => !active.has(t.ref), (slug) => (led.has(slug) ? idleGroupId(slug) : initiativeGroupId(slug)));
  return [...lead, ...rest.filter((s) => s.own.length > 0 || !led.has(s.slug))];
}

function initiativeGroup(section: Section, info: Timeline['initiatives'][number] | undefined, order: number): TimelineGroup {
  const { slug, own } = section;
  const tags = info === undefined || info.tags.length === 0 ? '' : ` (${info.tags.join(', ')})`;
  return {
    id: section.id,
    content: escapeHtml(info?.title ?? slug),
    order,
    className: info?.closed === true ? 'tl-initiative tl-initiative-closed' : 'tl-initiative',
    title: escapeHtml(slug + tags),
    nestedGroups: own.map((t) => taskGroupId(t.ref)),
    showNested: true,
    ref: slug,
    attentionLabel: '',
    tags: [],
  };
}

/** The planned and actual bars of a task, planned first. */
function spanItems(task: TimelineTask, nowIso: string): TimelineItem[] {
  const planned = plannedItem(task);
  return planned === undefined ? [actualItem(task, nowIso)] : [planned, actualItem(task, nowIso)];
}

function dependencyPairs(tasks: readonly TimelineTask[], held: ReadonlySet<string>): DependencyPair[] {
  return tasks.flatMap((task) =>
    task.deps
      .filter((blocker) => held.has(blocker))
      .map((blocker) => ({
        id: `dep:${blocker}>${task.ref}`,
        from: blocker,
        fromItem: actualItemId(blocker),
        to: task.ref,
        toItem: actualItemId(task.ref),
      })),
  );
}

/** The lease bars, each placed in its task's lease slots (R11) so overlapping leases stay readable and tappable. */
function leaseItems(leases: readonly TimelineLease[], nowIso: string): TimelineItem[] {
  const byTask = new Map<string, TimelineLease[]>();
  for (const lease of leases) byTask.set(lease.task, [...(byTask.get(lease.task) ?? []), lease]);
  const places = new Map<string, string>();
  for (const own of byTask.values()) {
    const spans = own.map((l) => ({ id: l.ref, start: Date.parse(l.started_at), end: Date.parse(leaseEnd(l, nowIso)) }));
    const { slots, count } = assignLeaseSlots(spans);
    for (const [ref, slot] of slots) places.set(ref, leaseSlotClass(slot, count));
  }
  return leases.map((l) => leaseItem(l, nowIso, places.get(l.ref)!));
}

/**
 * The groups, items and dependency pairs of an answer, its rows in the order `sections` gives; `now` ends the spans that are still open. Leases and markers
 * of a task the answer does not hold, and dependencies on one it does not hold, are left out (the window cut them).
 */
export function buildTimeline(timeline: TimelineAnswer, now: Date): TimelineModel {
  const nowIso = now.toISOString();
  const groups: TimelineGroup[] = [];
  const items: TimelineItem[] = [];
  const held = new Set(timeline.tasks.map((t) => t.ref));

  for (const section of sections(timeline)) {
    groups.push(initiativeGroup(section, timeline.initiatives.find((i) => i.slug === section.slug), groups.length));
    for (const task of section.own) {
      groups.push(taskGroup(task, groups.length, section.id, task.tag_marks));
      items.push(...spanItems(task, nowIso));
    }
  }
  items.push(...leaseItems(timeline.leases.filter((l) => held.has(l.task)), nowIso));
  items.push(...timeline.markers.flatMap((m, index) => (held.has(m.task) ? [markerItem(m, index)] : [])));
  return { groups, items, dependencies: dependencyPairs(timeline.tasks, held) };
}

/** The ref of the task a group id or item names, for opening its page; `undefined` for an initiative row. */
export function taskOfGroup(groupId: string): string | undefined {
  return groupId.startsWith('t:') ? groupId.slice(2) : undefined;
}

// --- filters ---

/** "Show closed within N days": 0 is off. */
export type ClosedWithin = 0 | 7 | 30 | 90;

export const CLOSED_WITHIN_OPTIONS: readonly { value: ClosedWithin; label: string }[] = [
  { value: 0, label: 'Off' },
  { value: 7, label: '7 days' },
  { value: 30, label: '30 days' },
  { value: 90, label: '90 days' },
];

export interface TimelineFilter {
  /** Initiative slugs, any of them. */
  initiatives: string[];
  /** Tags, all of them. */
  tags: string[];
  /** An actor handle; empty for any. */
  actor: string;
  closedWithin: ClosedWithin;
  /** Only active: keep the tasks that are active (the server's definition, SPEC 6.10). */
  activeOnly: boolean;
  /** The order of the rows: the current one, or Active first. */
  order: TimelineOrder;
}

export const DEFAULT_TIMELINE_FILTER: TimelineFilter = { initiatives: [], tags: [], actor: '', closedWithin: 0, activeOnly: false, order: 'default' };

/** The fragment's `active=1` is Only active; anything else is off. */
const ACTIVE_ONLY_ON = '1';

function closedFrom(raw: string | null): ClosedWithin {
  return CLOSED_WITHIN_OPTIONS.find((o) => String(o.value) === raw)?.value ?? 0;
}

/** The filter a query names; an unknown `closed` is off. */
export function filterFromQuery(query: string): TimelineFilter {
  const params = new URLSearchParams(query);
  return {
    initiatives: clean(params.getAll('initiative')),
    tags: clean(params.getAll('tag')),
    actor: (params.get('actor') ?? '').trim(),
    closedWithin: closedFrom(params.get('closed')),
    activeOnly: params.get('active') === ACTIVE_ONLY_ON,
    order: sortFromFragment(query, ACTIVE_COLUMNS) === null ? 'default' : 'active',
  };
}

/** The query of a filter for the URL fragment: only what is set, so the default is empty. */
export function filterToQuery(filter: TimelineFilter): string {
  const params = new URLSearchParams();
  for (const value of clean(filter.initiatives)) params.append('initiative', value);
  for (const value of clean(filter.tags)) params.append('tag', value);
  if (filter.actor.trim() !== '') params.set('actor', filter.actor.trim());
  if (filter.closedWithin !== 0) params.set('closed', String(filter.closedWithin));
  if (filter.activeOnly) params.set('active', ACTIVE_ONLY_ON);
  const sort = filter.order === 'active' ? sortFragment({ column: 'active', dir: 'desc' }) : '';
  return [params.toString(), sort].filter((part) => part !== '').join('&');
}

/** The fragment of the view under a filter: `#/timeline`, or `#/timeline?...` when a value is set. */
export function timelineHash(filter: TimelineFilter): string {
  const query = filterToQuery(filter);
  return query === '' ? '#/timeline' : `#/timeline?${query}`;
}

/** The filter held by a fragment: everything after the first `?` is the query. */
export function filterFromHash(hash: string): TimelineFilter {
  const at = hash.indexOf('?');
  return filterFromQuery(at < 0 ? '' : hash.slice(at + 1));
}

/**
 * The query of `GET /timeline` for a filter and the visible window. The rows are every task the filter keeps
 * (`rows=all`), so panning or zooming never adds, drops or reorders a row; the window picks the leases and markers.
 * `active` (the touched-in window of the Only active filter, 8h, 24h or 3d) adds `active=true&window=`.
 */
export function apiQuery(filter: TimelineFilter, from: Date, to: Date, active?: NowWindow): URLSearchParams {
  const params = new URLSearchParams({ from: from.toISOString(), to: to.toISOString(), rows: 'all' });
  for (const value of clean(filter.initiatives)) params.append('initiative', value);
  for (const value of clean(filter.tags)) params.append('tag', value);
  if (filter.actor.trim() !== '') params.set('actor', filter.actor.trim());
  if (filter.closedWithin !== 0) params.set('closed_within', `${filter.closedWithin}d`);
  if (active !== undefined) {
    params.set('active', 'true');
    params.set('window', active);
  }
  return params;
}

/** Whether any value is set (the view offers "clear filters" then). */
export function isFiltered(filter: TimelineFilter): boolean {
  return filterToQuery(filter) !== '';
}

// --- the filter controls ---

/** A select's options over a set of values: the "any" choice first, then the values sorted, the current one kept even when unknown. */
export function choiceOptions(values: readonly string[], current: string, any: string): { value: string; label: string }[] {
  const all = new Set(clean(values));
  if (current !== '') all.add(current);
  return [{ value: '', label: any }, ...[...all].sort().map((value) => ({ value, label: value }))];
}

/**
 * The tag chips Timeline offers: the tags the board's initiatives carry (marked as an initiative's), every tag of the task rows
 * drawn (own, initiative and derived actor-kind tags, each with its source), the ones already offered (`seen`: a narrower
 * read must not take a chip away) and the chosen ones.
 */
export function tagChoices(
  initiatives: readonly { tags: readonly string[] }[],
  chosen: readonly string[],
  rows: readonly { tags: readonly TagChoice[] }[] = [],
  seen: readonly TagChoice[] = [],
): TagChoice[] {
  const marks = initiatives.flatMap((i) => i.tags.map((tag) => ({ tag, source: 'initiative' as const })));
  return mergeChoices([...seen, ...rows.flatMap((r) => r.tags)], marks, chosen);
}

/** The filter with a closed-within choice read from a select's value. */
export function withClosedWithin(filter: TimelineFilter, raw: string): TimelineFilter {
  return { ...filter, closedWithin: closedFrom(raw) };
}

/** Whether a live event can change the timeline: a lease, task, ask or milestone event, a check, or a dependency. */
export function affectsTimeline(event: { type: string }): boolean {
  const { type } = event;
  return ['lease_', 'task_', 'ask_', 'milestone', 'dep_'].some((prefix) => type.startsWith(prefix)) || type === 'check_recorded';
}
