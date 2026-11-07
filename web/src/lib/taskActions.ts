// Which actions the task page offers (PRD R10): a finite table over the task's status and kind. The API refuses
// the others (409), so the page does not show them.
export type TaskActionStatus = 'open' | 'done' | 'closed';
export type TaskActionKind = 'work' | 'watch';

export interface TaskActions {
  edit: boolean;
  done: boolean;
  reopen: boolean;
  close: boolean;
  milestone: boolean;
  recordCheck: boolean;
}

/** The actions of a task: Edit and Done on a live task, Reopen on a finished one, Close unless closed, Milestone always, Record check on a watch task that is not closed. */
export function taskActions(status: string, kind: string): TaskActions {
  const closed = status === 'closed';
  return {
    edit: !closed,
    done: status === 'open',
    reopen: status === 'done' || closed,
    close: !closed,
    milestone: true,
    recordCheck: kind === 'watch' && !closed,
  };
}

/** The labels of the actions that are available, in the order the page lists them. */
export function actionLabels(actions: TaskActions): string[] {
  const labels: [boolean, string][] = [
    [actions.edit, 'Edit'],
    [actions.done, 'Done'],
    [actions.reopen, 'Reopen'],
    [actions.close, 'Close'],
    [actions.milestone, 'Milestone'],
    [actions.recordCheck, 'Record check'],
  ];
  return labels.filter(([on]) => on).map(([, label]) => label);
}
