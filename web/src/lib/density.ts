// The density preference (Compact or Sparse) of the list pages, and the controller that persists and applies it.
//
// Compact is the default: from 768 px the list pages drop the 44 px touch minimum and the roomy cell padding
// (styles.css, keyed on `data-density` of the root element). Sparse is the layout as it was. On a phone (below
// 768 px) both densities keep 44 px targets. A stored value this module does not know falls back to Compact.

export type Density = 'compact' | 'sparse';

export const DENSITIES: readonly Density[] = ['compact', 'sparse'];
export const DEFAULT_DENSITY: Density = 'compact';
/** The versioned storage key of the preference. */
export const DENSITY_STORAGE_KEY = 'vh.density.v1';

/** A stored value as a density: anything unknown (or absent) is Compact. */
export function parseDensity(raw: string | null | undefined): Density {
  return raw === 'sparse' ? 'sparse' : DEFAULT_DENSITY;
}

/** What the controller needs from the browser: tests pass fakes. */
export interface DensityEnv {
  storage: Pick<Storage, 'getItem' | 'setItem'>;
  root: { setAttribute: (name: string, value: string) => void };
}

export interface DensityController {
  density: () => Density;
  /** Chooses and persists a density, then applies it. */
  set: (density: Density) => void;
}

/** Reads the stored density, applies it to the root element, and persists each later choice. */
export function createDensityController(env: DensityEnv): DensityController {
  let current: Density = DEFAULT_DENSITY;
  try {
    current = parseDensity(env.storage.getItem(DENSITY_STORAGE_KEY));
  } catch {
    // Storage can be blocked: the page then runs on Compact.
  }
  const apply = () => env.root.setAttribute('data-density', current);
  apply();
  return {
    density: () => current,
    set(density) {
      current = density;
      try {
        env.storage.setItem(DENSITY_STORAGE_KEY, density);
      } catch {
        // The choice still applies to this page view.
      }
      apply();
    },
  };
}
