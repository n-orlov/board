<script lang="ts">
  import Dialog from './Dialog.svelte';
  import TaskBody from './TaskBody.svelte';
  import { currentBrowser } from './lib/session';
  import { affectsTaskPage, taskHref, TaskPageStore, type TaskPageState } from './lib/taskPage';
  import { TasksApi } from './lib/tasks';
  import { live } from './lib/live';

  // A task read over the Timeline, without leaving it: the task page's body, read-only (no action buttons), in the one
  // dialog (a bottom sheet on the phone). It reads the task like the task page does and follows the live stream the same way.
  let { csrf, ref, onclose }: { csrf: string; ref: string; onclose: () => void } = $props();

  let view: TaskPageState = $state({ task: undefined, events: [], expired: [], live: [], loading: true, error: undefined, actionError: undefined, candidates: [], asks: [], notes: [], bodies: {} });
  const store = new TaskPageStore(new TasksApi(currentBrowser(), () => csrf), (s) => (view = s));

  $effect(() => {
    void store.load(ref);
    return live().subscribe(affectsTaskPage(ref), () => store.refresh(ref));
  });

  const bar = 'inline-flex min-h-11 items-center rounded-md border border-border bg-surface-raised px-3 text-sm font-medium';
</script>

<Dialog label="Task {view.task?.ref ?? ref}" {onclose} initialFocus="[data-testid=preview-close]" wide>
  <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
    <a class="{bar} text-accent hover:underline" href={taskHref(view.task?.ref ?? ref)} data-testid="preview-open">Open full page</a>
    <button type="button" class="{bar} text-text" data-testid="preview-close" onclick={onclose}>Close</button>
  </div>
  <TaskBody {view} headingId="preview-heading" testid="task-preview" onexpand={(noteRef) => void store.loadBody(noteRef)} />
</Dialog>
