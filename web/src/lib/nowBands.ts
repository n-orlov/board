// The Needs me bands of the Now view (PRD R1, SPEC 11): the four bands in order and which of the attention sections
// each one holds, the count and rows of a band, which bands start open, and the open state a person leaves behind,
// remembered apart for wide and narrow screens under `vh.now.bands.v1`. In flight and Recently touched fold like the bands
// below 1024 px (a fold is a band or one of those two), with the same remembered state.
import type { AttentionCounts, SectionKey } from './attention';
import { rowsFor, type Attention, type ItemRow } from './attentionView';

export type BandKey = 'asks' | 'expired' | 'watches' | 'done';

export interface BandInfo {
  key: BandKey;
  title: string;
  /** The attention sections the band holds, in the order their items are listed. */
  sections: readonly SectionKey[];
  /** What the band says when it has no items. */
  empty: string;
}

/** What folds on a screen below the dock: the four bands, then In flight and Recently touched. */
export type FoldKey = BandKey | 'flight' | 'recent';

export const FOLD_KEYS: readonly FoldKey[] = ['asks', 'expired', 'watches', 'done', 'flight', 'recent'];

/** The bands in the order Needs me shows them; Done, not closed is last. */
export const BANDS: readonly BandInfo[] = [
  { key: 'asks', title: 'Open asks', sections: ['open_asks'], empty: 'No open asks. Nobody is waiting on an answer.' },
  {
    key: 'expired',
    title: 'Expired leases and conflicts',
    sections: ['expired_leases', 'conflicts'],
    empty: 'No expired leases to acknowledge and no conflicts.',
  },
  { key: 'watches', title: 'Overdue watches', sections: ['overdue_watches'], empty: 'No watch is overdue.' },
  { key: 'done', title: 'Done, not closed', sections: ['done_not_closed'], empty: 'Nothing is waiting to be closed.' },
];

/** The band that holds an attention section. */
export function bandOf(section: SectionKey): BandKey {
  return BANDS.find((band) => band.sections.includes(section))!.key;
}

/** The count a band shows: the counts of the sections it holds, added. */
export function bandCount(counts: AttentionCounts, band: BandInfo): number {
  return band.sections.reduce((sum, section) => sum + counts[section], 0);
}

/** One item of a band: the row, and the attention section it came from (which decides its actions). */
export interface BandRow {
  section: SectionKey;
  row: ItemRow;
}

/** The items of a band: those of its first section, then those of its second, each in the order the server gave them. */
export function bandRows(band: BandInfo, attention: Attention, now: Date): BandRow[] {
  return band.sections.flatMap((section) => rowsFor(section, attention, now).map((row) => ({ section, row })));
}

/** The screens the open state is remembered for: from 768 px (the density breakpoint) wide, below it narrow. */
export type Layout = 'wide' | 'narrow';

export const WIDE_QUERY = '(min-width: 768px)';

export function layoutOf(wide: boolean): Layout {
  return wide ? 'wide' : 'narrow';
}

/**
 * Whether a fold starts open when the person has not chosen. Wide: every one but Done, not closed, empty or not.
 * Narrow (an accordion): Open asks, Expired leases and conflicts and In flight when they hold something, the others
 * closed (Recently touched always starts closed, it is the quietest part).
 */
export function defaultOpen(key: FoldKey, layout: Layout, count: number): boolean {
  if (layout === 'wide') return key !== 'done';
  return (key === 'asks' || key === 'expired' || key === 'flight') && count > 0;
}

/** The choices made so far on one kind of screen: a band absent from it has not been toggled. */
export type BandChoices = Partial<Record<FoldKey, boolean>>;

export interface BandPrefs {
  wide: BandChoices;
  narrow: BandChoices;
}

export const NO_BAND_PREFS: BandPrefs = { wide: {}, narrow: {} };
/** The versioned storage key of the open state. */
export const BANDS_STORAGE_KEY = 'vh.now.bands.v1';

function choices(raw: unknown): BandChoices {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) return {};
  const out: BandChoices = {};
  for (const key of FOLD_KEYS) {
    const value = (raw as Record<string, unknown>)[key];
    if (typeof value === 'boolean') out[key] = value;
  }
  return out;
}

/** A stored value as the choices: anything that is not the shape written (or absent) is no choices at all. */
export function parseBandPrefs(raw: string | null | undefined): BandPrefs {
  if (raw === null || raw === undefined) return NO_BAND_PREFS;
  try {
    const value = JSON.parse(raw) as unknown;
    if (typeof value !== 'object' || value === null) return NO_BAND_PREFS;
    const { wide, narrow } = value as Record<string, unknown>;
    return { wide: choices(wide), narrow: choices(narrow) };
  } catch {
    return NO_BAND_PREFS;
  }
}

/** Whether a fold is open: the person's choice on this kind of screen, else the default. */
export function isOpen(prefs: BandPrefs, key: FoldKey, layout: Layout, count: number): boolean {
  return prefs[layout][key] ?? defaultOpen(key, layout, count);
}

/** The choices after a band is opened or closed on one kind of screen; the other kind is untouched. */
export function withChoice(prefs: BandPrefs, key: FoldKey, layout: Layout, open: boolean): BandPrefs {
  return { ...prefs, [layout]: { ...prefs[layout], [key]: open } };
}

/** The slice of `Storage` the preference uses. */
export type BandStorage = Pick<Storage, 'getItem' | 'setItem'>;

export function loadBandPrefs(storage: BandStorage | undefined): BandPrefs {
  try {
    return parseBandPrefs(storage?.getItem(BANDS_STORAGE_KEY));
  } catch {
    return NO_BAND_PREFS; // storage can be blocked
  }
}

export function saveBandPrefs(storage: BandStorage | undefined, prefs: BandPrefs): void {
  try {
    storage?.setItem(BANDS_STORAGE_KEY, JSON.stringify(prefs));
  } catch {
    // The choice still applies to this page view.
  }
}
