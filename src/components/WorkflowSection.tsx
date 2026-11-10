import React, { useEffect, useRef, useState } from 'react';
import { config } from '@grafana/runtime';
import { Alert, Button, Field, Input, Select } from '@grafana/ui';
import { errorStatus, fetchWorkflow, startWorkflow, transitionWorkflow } from '../api';
import { useI18n } from '../i18n';
import type { PolicyDoc, Problem, WorkflowStatus } from '../model';
import { roleAllows } from '../lib/workspace';

type WorkflowError = '' | 'error' | 'stale' | 'staleState' | 'staleRevision' | 'untrusted' | 'adminRequired';

interface Props {
  problem: Problem;
  policy: PolicyDoc;
  canWrite: boolean;
  onChanged: () => void;
}

export function WorkflowSection({ problem, policy, canWrite, onChanged }: Props) {
  const { language, messages: m } = useI18n();
  const [response, setResponse] = useState<{ problemId: string; status: WorkflowStatus }>();
  const status = response?.problemId === problem.id ? response.status : undefined;
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<WorkflowError>('');
  const [loadError, setLoadError] = useState(false);
  const [missing, setMissing] = useState<string[]>([]);
  const [choice, setChoice] = useState('');
  const [comment, setComment] = useState('');
  const [attempt, setAttempt] = useState(0);
  const drafts = useRef(new Map<string, string>());
  const fetchSequence = useRef(0);
  const currentId = useRef(problem.id);
  currentId.current = problem.id;
  const actorRole = config.bootData.user.orgRole;

  useEffect(() => {
    setComment(drafts.current.get(problem.id) ?? '');
    setResponse(undefined);
    setError('');
    setLoadError(false);
    setMissing([]);
    setChoice('');
  }, [problem.id]);

  useEffect(() => {
    let active = true;
    const sequence = ++fetchSequence.current;
    const isCurrent = () => active && sequence === fetchSequence.current;
    setLoading(true);
    setLoadError(false);
    fetchWorkflow(problem.id).then((value) => {
      if (!isCurrent()) return;
      setResponse({ problemId: problem.id, status: value });
      if (!value.tracked) setChoice((previous) => value.availableWorkflows.some((workflow) => workflow.id === previous) ? previous : value.suggestedWorkflow || (value.availableWorkflows.length === 1 ? value.availableWorkflows[0].id : ''));
    }).catch(() => { if (isCurrent()) { setResponse(undefined); setLoadError(true); } }).finally(() => { if (isCurrent()) setLoading(false); });
    return () => { active = false; };
  }, [problem.id, problem.status, problem.acknowledgedBy, problem.assignee, attempt]);

  function gateLabel(gate: string): string {
    if (gate === 'ack') return m.workflow.ack;
    if (gate === 'owner') return m.workflow.owner;
    if (gate === 'comment') return m.workflow.comment;
    if (gate === 'sourceRecovered') return m.workflow.sourceRecovered;
    if (gate === 'role') return m.workflow.adminRequired;
    return gate;
  }

  async function mutate(snapshot: WorkflowStatus, to?: string, selected?: string) {
    const id = problem.id;
    const submitted = comment;
    if (!canWrite || busy || loading) return;
    if (snapshot.tracked) {
      const edge = snapshot.edges.find((item) => item.to === to);
      if (!edge || !roleAllows(actorRole, edge.requiredRole)) return;
    } else {
      const workflow = snapshot.availableWorkflows.find((item) => item.id === selected);
      if (!workflow || !roleAllows(actorRole, workflow.minimumRole)) return;
    }
    setBusy(true);
    setError('');
    setMissing([]);
    try {
      const result = snapshot.tracked
        ? await transitionWorkflow(id, snapshot.stateId, to ?? '', snapshot.revisionUsed, submitted)
        : await startWorkflow(id, selected, snapshot.policyRevision, submitted);
      if (currentId.current !== id) return;
      fetchSequence.current += 1;
      setLoading(false);
      setResponse({ problemId: id, status: result.workflow });
      if (drafts.current.get(id) === submitted) {
        drafts.current.delete(id);
        setComment('');
      }
      onChanged();
    } catch (requestError) {
      if (currentId.current !== id) return;
      const data = (requestError as { data?: { error?: unknown; missing?: unknown } })?.data;
      if (Array.isArray(data?.missing)) setMissing(data.missing.filter((item): item is string => typeof item === 'string'));
      const code = errorStatus(requestError);
      setError(code === 409 ? data?.error === 'source_state_untrusted' ? 'untrusted' : data?.error === 'stale_state' ? 'staleState' : data?.error === 'stale_revision' ? 'staleRevision' : 'stale' : code === 403 && data?.error === 'admin_role_required' ? 'adminRequired' : 'error');
      if (code === 409) { fetchSequence.current += 1; setResponse(undefined); setLoading(true); setAttempt((value) => value + 1); }
    } finally {
      setBusy(false);
    }
  }

  const workflow = status?.tracked ? policy.workflows.find((item) => item.id === status.workflowId) : undefined;
  const state = status?.tracked ? workflow?.states.find((item) => item.id === status.stateId) : undefined;
  const name = (id: string) => workflow?.states.find((item) => item.id === id)?.name[language] ?? m.summary.retired(id);
  const commentRequired = status?.tracked && status.edges.some((edge) => edge.missing.includes('comment') || workflow?.transitions.some((transition) => transition.from === status.stateId && transition.to === edge.to && transition.requires.comment));
  const selectedWorkflow = status && !status.tracked ? status.availableWorkflows.find((item) => item.id === choice) : undefined;

  return <section aria-labelledby="workflow-heading">
    <h3 id="workflow-heading">{m.workflow.title}</h3>
    {loading && <p>{m.common.loading}</p>}
    {error && <Alert severity="error" title={m.workflow.error}>{m.workflow[error]}{missing.length > 0 && <p>{m.workflow.missing}: {missing.map(gateLabel).join(', ')}</p>}</Alert>}
    {loadError && <Alert severity="error" title={m.workflow.error}><Button variant="secondary" size="sm" onClick={() => setAttempt((value) => value + 1)}>{m.common.retry}</Button></Alert>}
    {status && !status.tracked && <>
      <p>{m.workflow.untracked}</p>
      {canWrite && <Field label={m.workflow.select}><Select aria-label={m.workflow.select} value={choice} placeholder={m.workflow.choose} options={status.availableWorkflows.map((item) => ({ value: item.id, label: `${item.name[language]} (${item.minimumRole === 'admin' ? m.policy.roleAdmin : m.policy.roleEditor})` }))} onChange={(option) => setChoice(option.value ?? '')} /></Field>}
      {canWrite && selectedWorkflow && !roleAllows(actorRole, selectedWorkflow.minimumRole) && <p>{m.workflow.adminRequired}</p>}
    </>}
    {status?.tracked && <>
      <p>{workflow?.name[language] ?? m.summary.retired(status.workflowId)} · {state?.name[language] ?? m.summary.retired(status.stateId)}</p>
      {status.retired && <Alert title={m.workflow.retired} severity="warning" />}
      {status.sourceConflict && <Alert title={m.workflow.conflict} severity="warning" />}
    </>}
    {canWrite && status && (!status.tracked || commentRequired) && <Field label={m.workflow.comment}><Input aria-label={m.workflow.comment} value={comment} onChange={(event) => { const value = event.currentTarget.value; drafts.current.set(problem.id, value); setComment(value); }} /></Field>}
    {canWrite && status && !status.tracked && <Button size="sm" disabled={busy || loading || !selectedWorkflow || !roleAllows(actorRole, selectedWorkflow.minimumRole)} onClick={() => void mutate(status, undefined, choice)}>{m.workflow.start}</Button>}
    {status?.tracked && status.edges.map((edge) => {
      const missingWithoutComment = edge.missing.filter((gate) => gate !== 'comment');
      const enabled = roleAllows(actorRole, edge.requiredRole) && (edge.allowed || (missingWithoutComment.length === 0 && edge.missing.includes('comment') && Boolean(comment.trim())));
      return <div key={edge.to}>
        <Button size="sm" variant="secondary" disabled={!canWrite || busy || loading || !enabled} onClick={() => void mutate(status, edge.to)}>{name(edge.to)}</Button>
        {edge.missing.length > 0 && <span> {m.workflow.missing}: {edge.missing.map(gateLabel).join(', ')}</span>}
        {canWrite && !roleAllows(actorRole, edge.requiredRole) && !edge.missing.includes('role') && <span> {m.workflow.adminRequired}</span>}
      </div>;
    })}
  </section>;
}
