// Which restored tag chips are still valid (PRD R7/R10, SPEC 11). A saved filter may name a tag an initiative lists, a task's own
// tag or a derived actor-kind tag. The board's list of initiatives knows only the first; the rest are valid when a task matches
// them under the server's own rule (`GET /tasks?tag=`: own, initiative or actor kind), so this asks the server instead of
// re-deriving the rule.

/** Whether any task matches the tag under the server's rule; it rejects when the answer cannot be had. */
export type TagProbe = (tag: string) => Promise<boolean>;

/**
 * The tags among `tags` that some task carries, among those no initiative of `known` lists (the initiatives' own tags need no
 * probe). A probe that fails keeps its tag: what cannot be checked is not dropped, the view shows the server's answer for it.
 */
export async function carriedTags(
  tags: readonly string[],
  known: readonly { tags: readonly string[] }[],
  probe: TagProbe,
): Promise<ReadonlySet<string>> {
  const listed = new Set(known.flatMap((i) => i.tags));
  const unlisted = [...new Set(tags)].filter((tag) => !listed.has(tag));
  const answers = await Promise.all(unlisted.map((tag) => probe(tag).catch(() => true)));
  return new Set(unlisted.filter((_, n) => answers[n]));
}
