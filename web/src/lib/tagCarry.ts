// Which restored tag chips are still valid, and where each comes from (PRD R7/R10, SPEC 11). A saved filter may name a tag an
// initiative lists, a task's own tag or a derived actor-kind tag. The board's list of initiatives knows only the first; the rest
// are valid when a task matches them under the server's own rule (`GET /tasks?tag=`: own, initiative or actor kind), so this asks
// the server instead of re-deriving the rule. The same answer names the sources the tag has on those tasks, so a restored chip
// keeps its accessible name and hover text whether or not the combined filter returns a row.

import type { TagMark } from './chipFilter';

/** The marks of a tag on the tasks that match it (one per source); empty when no task matches. Rejects when the answer cannot be had. */
export type TagProbe = (tag: string) => Promise<readonly TagMark[]>;

/** The restored tags some task carries, and the marks (each with its source) the probe found for every restored tag. */
export interface Carried {
  /** Valid without an initiative listing them: a task matches them, or the check could not be made. */
  tags: ReadonlySet<string>;
  marks: TagMark[];
}

/**
 * What the restored `tags` carry. Every tag is probed once, the initiatives' own too, because a tag an initiative lists may also be
 * a task's own and the chip names both. A tag an initiative of `known` lists needs no probe to be valid. A probe that fails keeps
 * its tag, with no marks: what cannot be checked is not dropped, the view shows the server's answer for it.
 */
export async function carriedTags(
  tags: readonly string[],
  known: readonly { tags: readonly string[] }[],
  probe: TagProbe,
): Promise<Carried> {
  const listed = new Set(known.flatMap((i) => i.tags));
  const wanted = [...new Set(tags)];
  const answers = await Promise.all(wanted.map((tag) => probe(tag).then((marks) => ({ marks, failed: false }), () => ({ marks: [] as readonly TagMark[], failed: true }))));
  const carried = new Set<string>();
  const marks: TagMark[] = [];
  wanted.forEach((tag, n) => {
    const answer = answers[n]!;
    marks.push(...answer.marks);
    if (!listed.has(tag) && (answer.failed || answer.marks.length > 0)) carried.add(tag);
  });
  return { tags: carried, marks };
}
