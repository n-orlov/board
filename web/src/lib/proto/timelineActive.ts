import type { TimelineGroup, TimelineModel } from '../timeline';

// Prototype: the Timeline's client-side shaping. The server answers every row the filter keeps; this drops the rows the
// browser-only filters reject (actor and manual tags, "only active") and moves the active rows first when asked.

export type TimelineSort = 'default' | 'active';
export const TIMELINE_SORTS: readonly { value: TimelineSort; label: string }[] = [
  { value: 'default', label: 'Initiative order' },
  { value: 'active', label: 'Active first' },
];
export const isTimelineSort = (v: unknown): v is TimelineSort => v === 'default' || v === 'active';

/** Why a task counts as active: a live lease, an attention reason, or touched within the chosen window. */
export interface Activity {
  /** Lower ranks first: 0 live lease, 1 needs attention, 2 recently touched. */
  rank: number;
  /** The newest moment the task was active, for ties (ms). */
  at: number;
}

export interface Shape {
  keep: (ref: string) => boolean;
  activity: (ref: string) => Activity | undefined;
  onlyActive: boolean;
  sort: TimelineSort;
}

function byActivity(a: Activity | undefined, b: Activity | undefined): number {
  if (a === undefined || b === undefined) return a === undefined ? (b === undefined ? 0 : 1) : -1;
  return a.rank - b.rank || b.at - a.at;
}

/** The model with the rows the shape keeps, in its order; items and arrows of dropped rows go too. */
export function shapeModel(model: TimelineModel, shape: Shape): TimelineModel {
  if (!shape.onlyActive && shape.sort === 'default' && model.groups.every((g) => g.nestedGroups !== undefined || shape.keep(g.ref))) return model;
  const tasks = new Map(model.groups.filter((g) => g.nestedGroups === undefined).map((g) => [g.id, g] as const));
  const wanted = (g: TimelineGroup) => shape.keep(g.ref) && (!shape.onlyActive || shape.activity(g.ref) !== undefined);
  const blocks: { head: TimelineGroup; rows: TimelineGroup[]; best: Activity | undefined }[] = [];
  for (const head of model.groups) {
    if (head.nestedGroups === undefined) continue;
    let rows = head.nestedGroups.map((id) => tasks.get(id)).filter((g): g is TimelineGroup => g !== undefined && wanted(g));
    if (rows.length === 0) continue;
    if (shape.sort === 'active') rows = [...rows].sort((a, b) => byActivity(shape.activity(a.ref), shape.activity(b.ref)));
    const best = rows.map((r) => shape.activity(r.ref)).sort(byActivity)[0];
    blocks.push({ head, rows, best });
  }
  if (shape.sort === 'active') blocks.sort((a, b) => byActivity(a.best, b.best));
  const groups: TimelineGroup[] = [];
  for (const block of blocks) {
    groups.push({ ...block.head, order: groups.length, nestedGroups: block.rows.map((r) => r.id) });
    for (const row of block.rows) {
      const active = shape.activity(row.ref);
      const cls = active === undefined ? row.className : `${row.className} tl-active tl-active-${active.rank}`;
      groups.push({ ...row, order: groups.length, className: cls });
    }
  }
  const held = new Set(groups.map((g) => g.id));
  const refs = new Set(groups.filter((g) => g.nestedGroups === undefined).map((g) => g.ref));
  return {
    groups,
    items: model.items.filter((i) => held.has(i.group)),
    dependencies: model.dependencies.filter((d) => refs.has(d.from) && refs.has(d.to)),
  };
}
