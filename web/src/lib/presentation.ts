// How statuses and tags are presented (PRD R6/R7): the statuses of the two lists map to a badge tone, a
// tone maps to token-based Tailwind utilities (never a colour value), and tags map to chip labels.
import type { Initiative } from './initiatives';
import type { Key } from './keys';

export type Tone = 'success' | 'neutral' | 'warning' | 'danger';

const INITIATIVE_TONES: Record<Initiative['status'], Tone> = {
  open: 'success',
  closed: 'neutral',
};

const KEY_TONES: Record<Key['status'], Tone> = {
  active: 'success',
  revoked: 'danger',
  expired: 'warning',
};

const TONE_CLASSES: Record<Tone, string> = {
  success: 'border-success text-success',
  neutral: 'border-border text-text-muted',
  warning: 'border-warning text-warning',
  danger: 'border-danger text-danger',
};

function lookup(table: Record<string, Tone>, status: string): Tone {
  return Object.hasOwn(table, status) ? table[status] : 'neutral';
}

/** The badge tone of an initiative status; a status the table does not know is neutral. */
export function initiativeTone(status: string): Tone {
  return lookup(INITIATIVE_TONES, status);
}

/** The badge tone of an API key status; a status the table does not know is neutral. */
export function keyTone(status: string): Tone {
  return lookup(KEY_TONES, status);
}

/** The utilities a badge of this tone carries, all from the design tokens. */
export function toneClasses(tone: Tone): string {
  return TONE_CLASSES[tone];
}

/** The chips for a tag list: each tag trimmed, blank ones dropped, order kept. */
export function tagLabels(tags: readonly string[]): string[] {
  return tags.map((tag) => tag.trim()).filter((tag) => tag !== '');
}
