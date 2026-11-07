// The words of the initiative page's close and reopen dialogs (PRD R9, SPEC 11). The page holds every task of
// the initiative, so it can say how many a close would drop before asking the server.

/** How many of the tasks are not closed (open or done): what a forced close drops. */
export function notClosedCount(tasks: readonly { status: string }[]): number {
  return tasks.filter((task) => task.status !== 'closed').length;
}

const tasksText = (n: number) => `${n} ${n === 1 ? 'task' : 'tasks'}`;

/** What the close confirmation says: the plain effect, or how many tasks a close would drop. */
export function closeBody(notClosed: number): string {
  if (notClosed === 0) return 'Closing stops new tasks and edits to its tasks until it is reopened.';
  return `${tasksText(notClosed)} not closed. Closing drops ${notClosed === 1 ? 'it' : 'them'} too: each is closed as dropped.`;
}

/** The confirm button of the close dialog: "Close initiative", or "Close and drop 2 tasks" when some are not closed. */
export function closeLabel(notClosed: number): string {
  return notClosed === 0 ? 'Close initiative' : `Close and drop ${tasksText(notClosed)}`;
}

/** The toast after a close; `dropped` is how many tasks it dropped. */
export function closedToast(slug: string, dropped: number): string {
  return dropped === 0 ? `Initiative ${slug} closed` : `Initiative ${slug} closed, ${tasksText(dropped)} dropped`;
}

export const REOPEN_BODY = 'Reopening lets it take tasks again. Its closed tasks stay closed.';
