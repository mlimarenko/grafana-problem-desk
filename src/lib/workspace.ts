import { pageCount } from './query';
import type { MinimumRole } from '../model';

export function roleAllows(actor: string | undefined, required: MinimumRole): boolean {
  return actor === 'Admin' || (actor === 'Editor' && required === 'editor');
}

export function correctedOffset(offset: number, limit: number, total: number): number {
  return Math.min(offset, (pageCount(total, limit) - 1) * limit);
}

export function clearSubmittedDraft(drafts: Record<string, string>, problemId: string, submittedDraft: string): Record<string, string> {
  if (!Object.hasOwn(drafts, problemId) || drafts[problemId] !== submittedDraft) {
    return drafts;
  }
  const next: Record<string, string> = Object.assign(Object.create(null), drafts);
  delete next[problemId];
  return next;
}

export async function submitCommentDraft(
  problemId: string,
  submittedDraft: string,
  post: (problemId: string, body: string) => Promise<unknown>,
  updateDrafts: (update: (drafts: Record<string, string>) => Record<string, string>) => void
): Promise<void> {
  await post(problemId, submittedDraft.trim());
  updateDrafts((drafts) => clearSubmittedDraft(drafts, problemId, submittedDraft));
}

export function validLabelFilter(key: string, value: string): boolean {
  const encoder = new TextEncoder();
  return (key !== '' || value === '') && encoder.encode(key).length <= 256 && encoder.encode(value).length <= 1024;
}
