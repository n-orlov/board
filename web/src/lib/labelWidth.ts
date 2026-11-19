// The Timeline's label column (PRD R8, SPEC 11): its width is what the user set with the drag handle or the arrow keys,
// held between a minimum and a share of the chart's width, and remembered under `vh.timeline.labelwidth.v1`. This module is
// the pure logic: the clamp, the stored value's parse, and the keyboard step.

export const LABEL_DEFAULT = 300;
export const LABEL_MIN = 120;
export const LABEL_STEP = 16;
/** The widest the column gets, as a share of the chart's width. */
export const LABEL_MAX_SHARE = 0.6;
/** The versioned storage key of the width. */
export const LABEL_STORAGE_KEY = 'vh.timeline.labelwidth.v1';

/** The widest column a chart `chartWidth` px wide allows (never below the minimum). */
export function labelMax(chartWidth: number): number {
  if (!Number.isFinite(chartWidth)) return LABEL_MIN;
  return Math.max(LABEL_MIN, Math.floor(chartWidth * LABEL_MAX_SHARE));
}

/** `px` held between the minimum and the share of the chart's width; a value that is no number gives the default (itself held). */
export function clampLabelWidth(px: number, chartWidth: number): number {
  const value = Number.isFinite(px) ? Math.round(px) : LABEL_DEFAULT;
  return Math.min(labelMax(chartWidth), Math.max(LABEL_MIN, value));
}

/** A stored value as a width: whole pixels at or above the minimum only, anything else (or absent) is the default. */
export function parseLabelWidth(raw: string | null | undefined): number {
  if (raw === null || raw === undefined || !/^\d{1,5}$/.test(raw)) return LABEL_DEFAULT;
  const px = Number(raw);
  return px >= LABEL_MIN ? px : LABEL_DEFAULT;
}

/** The width a key moves the column to, or undefined for a key that does not resize it (Home resets, End is the widest). */
export function labelWidthForKey(key: string, width: number, chartWidth: number): number | undefined {
  switch (key) {
    case 'ArrowLeft':
      return clampLabelWidth(width - LABEL_STEP, chartWidth);
    case 'ArrowRight':
      return clampLabelWidth(width + LABEL_STEP, chartWidth);
    case 'Home':
      return LABEL_MIN;
    case 'End':
      return labelMax(chartWidth);
    default:
      return undefined;
  }
}

/** The slice of `Storage` the preference uses. */
export type LabelStorage = Pick<Storage, 'getItem' | 'setItem'>;

export function loadLabelWidth(storage: LabelStorage | undefined): number {
  try {
    return parseLabelWidth(storage?.getItem(LABEL_STORAGE_KEY));
  } catch {
    return LABEL_DEFAULT; // storage can be blocked
  }
}

export function saveLabelWidth(storage: LabelStorage | undefined, width: number): void {
  try {
    storage?.setItem(LABEL_STORAGE_KEY, String(width));
  } catch {
    // The width still applies to this page view.
  }
}
