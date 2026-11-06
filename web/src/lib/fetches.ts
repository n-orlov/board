// The loading rule both list pages share (SPEC 11): a list is loading while ANY fetch of it is
// pending, the first or a refresh after a create or a revoke, and only once every one of them has
// succeeded or failed is it not. Fetches can overlap (a create submitted while the first fetch is
// still out) and can land out of order; the outcome of a fetch that lands after a newer one has
// already landed is stale and is dropped, so the page never goes back to an older list.

export class Fetches {
  private pending = 0;
  private started = 0;
  private landed = 0;

  /** Records that a fetch started; the ticket is what `end` takes when it lands. */
  begin(): number {
    this.pending += 1;
    this.started += 1;
    return this.started;
  }

  /** Records that the fetch with `ticket` landed (worked or failed); whether its outcome is to be shown. */
  end(ticket: number): boolean {
    this.pending -= 1;
    const newest = ticket > this.landed;
    if (newest) this.landed = ticket;
    return newest;
  }

  /** Whether any fetch is still pending: the list's `loading`. */
  get busy(): boolean {
    return this.pending > 0;
  }
}

/** The text of a thrown value: an Error's message, else the value itself. */
export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
