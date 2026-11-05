// The views the app switches between, chosen by the URL fragment so a reload and a copied link
// land on the same view and the server needs no route for it.
export type View = 'initiatives' | 'keys';

export const KEYS_HASH = '#/settings/keys';

/** The view a fragment names; anything it does not name is the default, the initiatives list. */
export function viewFromHash(hash: string): View {
  return hash === KEYS_HASH ? 'keys' : 'initiatives';
}
