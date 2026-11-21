// Which restored tag chips are still valid, and where each comes from (PRD R7/R10, SPEC 11). A saved filter may name a tag an
// initiative lists, a task's own tag or a derived actor-kind tag. The board's list of initiatives knows only the first; the rest
// are valid when a task matches them under the server's own rule (`GET /tasks?tag=`: own, initiative or actor kind), so this asks
// the server instead of re-deriving the rule. The same answer names the sources the tag has on those tasks, so a restored chip
// keeps its accessible name and hover text whether or not the combined filter returns a row.

import { canonicalTag, type TagMark } from './chipFilter';
import { TAG_PATTERN } from './taskTags';

/** The marks of a tag on the tasks that match it (one per source); empty when no task matches. Rejects when the answer cannot be had. */
export type TagProbe = (tag: string) => Promise<readonly TagMark[]>;

/** The restored tags some task carries, and the marks (each with its source) the probe found for every restored tag. */
export interface Carried {
  /** Valid without an initiative listing them: a task matches them, or the check could not be made. */
  tags: ReadonlySet<string>;
  marks: TagMark[];
  /** The tags the probe answered for: `marks` holds every source each of them has now (a failed probe's tag is not here). */
  answered: ReadonlySet<string>;
}

/**
 * What the restored `tags` carry (in the server's spelling). Every tag is probed once, the initiatives' own too, because a tag an initiative lists may also be
 * a task's own and the chip names both. A tag an initiative of `known` lists needs no probe to be valid. A probe that fails keeps
 * its tag, with no marks: what cannot be checked is not dropped, the view shows the server's answer for it.
 */
export async function carriedTags(
  tags: readonly string[],
  known: readonly { tags: readonly string[] }[],
  probe: TagProbe,
): Promise<Carried> {
  const listed = new Set(known.flatMap((i) => i.tags.map(canonicalTag)));
  // Each tag in the server's spelling, once; one the server's grammar refuses cannot be on any task or initiative, so it is unknown
  // without asking (the server would answer 400, which is a failed check and would keep it).
  const wanted = [...new Set(tags.map(canonicalTag))].filter((tag) => TAG_PATTERN.test(tag));
  const answers = await Promise.all(wanted.map((tag) => probe(tag).then((marks) => ({ marks, failed: false }), () => ({ marks: [] as readonly TagMark[], failed: true }))));
  const carried = new Set<string>();
  const answered = new Set<string>();
  const marks: TagMark[] = [];
  wanted.forEach((tag, n) => {
    const answer = answers[n]!;
    marks.push(...answer.marks);
    if (!answer.failed) answered.add(tag);
    if (!listed.has(tag) && (answer.failed || answer.marks.length > 0)) carried.add(tag);
  });
  return { tags: carried, marks, answered };
}

/** One source read's answer: the `Carried` of its tags, and the part of it that still holds when it is applied. */
export interface SourceRead extends Carried {
  /**
   * The answered tags and their marks, less every tag a newer read has begun for since this one began: an older answer landing
   * after a newer one (or while it is out) must not put back a source the newer one no longer reports. Asked when the answer is
   * applied, not when it lands, so a wait in between (the catalog's read) cannot let a stale answer through.
   */
  fresh(): Pick<Carried, 'answered' | 'marks'>;
}

/**
 * The per-tag source reads of one view, in the order they began (PRD R7/R10, SPEC 11). A link, a restoration and a hash change
 * each read the sources of their tags; their answers may land in any order, and only the newest read of a tag names its sources.
 * The validity of a restored tag (`tags`) is the read's own: the view's filter turns already drop a restore that is no longer current.
 */
export class SourceReads {
  private began = 0;
  private readonly newest = new Map<string, number>();

  constructor(private readonly probe: TagProbe) {}

  /** Begins a read of `tags` (now, before it awaits anything) against the initiatives of `known`. */
  async read(tags: readonly string[], known: readonly { tags: readonly string[] }[] = []): Promise<SourceRead> {
    const turn = ++this.began;
    for (const tag of tags) this.newest.set(canonicalTag(tag), turn);
    const carried = await carriedTags(tags, known, this.probe);
    const current = (tag: string) => this.newest.get(tag) === turn;
    return {
      ...carried,
      fresh: () => ({ answered: new Set([...carried.answered].filter(current)), marks: carried.marks.filter((m) => current(m.tag)) }),
    };
  }
}
