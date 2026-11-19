// UX prototype: per-browser preferences (density, manual tags, chip filters, Now's sort and window).
// Everything is in localStorage under `vh.proto.*`; every access is guarded, and the page works without it.

export type Density = 'compact' | 'sparse';

function read<T>(key: string, fallback: T, valid: (v: unknown) => boolean): T {
  try {
    const raw = window.localStorage.getItem(key);
    if (raw === null) return fallback;
    const value: unknown = JSON.parse(raw);
    return valid(value) ? (value as T) : fallback;
  } catch {
    return fallback;
  }
}

function write(key: string, value: unknown): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // private window or blocked storage: the choice lasts for this page only
  }
}

const isStringList = (v: unknown) => Array.isArray(v) && v.every((x) => typeof x === 'string');
const isTagMap = (v: unknown) => typeof v === 'object' && v !== null && !Array.isArray(v) && Object.values(v).every(isStringList);

export const prefs = $state({
  density: read<Density>('vh.proto.density', 'compact', (v) => v === 'compact' || v === 'sparse'),
  manual: read<Record<string, string[]>>('vh.proto.manualTags', {}, isTagMap),
});

export function applyDensity(): void {
  document.documentElement.dataset.density = prefs.density;
}

export function setDensity(density: Density): void {
  prefs.density = density;
  write('vh.proto.density', density);
  applyDensity();
}

export function manualTagsOf(ref: string): string[] {
  return prefs.manual[ref] ?? [];
}

export function addManualTag(ref: string, tag: string): void {
  const clean = tag.trim().toLowerCase().replace(/\s+/g, '-');
  if (clean === '') return;
  const list = manualTagsOf(ref);
  if (list.includes(clean)) return;
  prefs.manual = { ...prefs.manual, [ref]: [...list, clean] };
  write('vh.proto.manualTags', prefs.manual);
}

export function removeManualTag(ref: string, tag: string): void {
  const list = manualTagsOf(ref).filter((t) => t !== tag);
  const next = { ...prefs.manual };
  if (list.length === 0) delete next[ref];
  else next[ref] = list;
  prefs.manual = next;
  write('vh.proto.manualTags', prefs.manual);
}

/** A per-view preference (a chip filter, a sort, a window). */
export function loadPref<T>(name: string, fallback: T, valid: (v: unknown) => boolean = () => true): T {
  return read(`vh.proto.${name}`, fallback, valid);
}

export function savePref(name: string, value: unknown): void {
  write(`vh.proto.${name}`, value);
}

export const isChipState = (v: unknown) =>
  typeof v === 'object' && v !== null && isStringList((v as { tags?: unknown }).tags) && isStringList((v as { initiatives?: unknown }).initiatives);
