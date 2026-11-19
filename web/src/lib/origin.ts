// The origin display rule (PRD R13, SPEC 11): an event or an activity line says "via <origin>" only when the operator
// behind the write is somebody other than the actor it was written as. An origin equal to the actor (an operator's own
// session write) or no origin at all (a key without an owner, an event from before origin was recorded) adds nothing.

/** The text "via alice" for a line written as `actor` on behalf of `origin`; the empty string when nothing is to be said. */
export function viaOrigin(actor: string, origin: string | null | undefined): string {
  if (origin === undefined || origin === null || origin === '' || origin === actor) return '';
  return `via ${origin}`;
}

/** What an Actors row says about the operator behind an agent: its handle, or `undefined` for an operator or no origin. */
export function actorOrigin(kind: string, origin: string | null | undefined): string | undefined {
  if (kind !== 'agent' || origin === undefined || origin === null || origin === '') return undefined;
  return origin;
}

/** What a key row says about its owner: the handle, or a plain "no owner" for a key made without one. */
export function keyOwnerText(owner: string | null | undefined): string {
  return owner === undefined || owner === null || owner === '' ? 'no owner' : owner;
}
