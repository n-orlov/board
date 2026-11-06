// Copying the once-shown API key (PRD R7). The Clipboard API is only there in a secure context and
// can refuse (permission, an unfocused page), so the copy answers whether it worked instead of throwing.

export interface ClipboardLike {
  writeText(text: string): Promise<void>;
}

/** Writes `text` to the clipboard. False when there is no Clipboard API or the write was refused. */
export async function copyText(clipboard: ClipboardLike | undefined, text: string): Promise<boolean> {
  if (clipboard === undefined) return false;
  try {
    await clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
