// UX prototype: the Timeline's label column is as wide as the user drags it (this module is its pure logic).

export const LABEL_DEFAULT = 300;
export const LABEL_MIN = 120;
export const LABEL_STEP = 16;
/** The widest the column gets, as a share of the chart's width. */
export const LABEL_MAX_SHARE = 0.6;

/** The widest column a chart `chartWidth` px wide allows (never below the minimum). */
export function labelMax(chartWidth: number): number {
  return Math.max(LABEL_MIN, Math.floor(chartWidth * LABEL_MAX_SHARE));
}

/** `px` held between the minimum and 60 % of the chart's width; anything that is no number gives the default. */
export function clampLabelWidth(px: number, chartWidth: number): number {
  if (!Number.isFinite(px)) return LABEL_DEFAULT;
  return Math.min(labelMax(chartWidth), Math.max(LABEL_MIN, Math.round(px)));
}

export const isLabelWidth = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v >= LABEL_MIN && v <= 4000;
