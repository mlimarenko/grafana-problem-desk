import React, { useCallback, useEffect, useReducer, useState } from 'react';
import { css } from '@emotion/css';
import type { GrafanaTheme2 } from '@grafana/data';
import { Alert, Button, Checkbox, ConfirmModal, Field, Input, Select, TextArea, useStyles2 } from '@grafana/ui';
import { errorStatus, fetchPolicy, fetchPolicyRevision, fetchPolicyRevisions, previewPolicy, savePolicy } from '../api';
import { useI18n } from '../i18n';
import type { PolicyMessages } from '../i18n/messages';
import { documentFromDraft, draftFromPolicy, labelsFromRows, localizePolicyIssue, parsePolicyJSON, policySaveRevision, reducePolicyEditor, type LabelRow, type PolicyDraft } from '../lib/policy';
import type { LevelTone, PolicyColumn, PolicyDoc, PolicyIssue, PolicyPreview, PolicyPreviewSample, PolicyRange, PolicyRevision, Requires } from '../model';

const tones: LevelTone[] = ['red', 'orange', 'yellow', 'blue', 'green', 'purple', 'grey'];
const columns: PolicyColumn[] = ['level', 'name', 'status', 'started', 'duration', 'owner', 'ack'];
const ranges: PolicyRange[] = ['1h', '24h', '7d', '30d'];
const gates: Array<keyof Requires> = ['ack', 'owner', 'comment', 'sourceRecovered'];

interface SampleDraft { labels: LabelRow[]; acknowledged: boolean; assignee: string; status: '' | 'active' | 'recovered'; comment: string }
type ErrorKey = '' | 'loadFailed' | 'requestFailed' | 'validationFailed' | 'importFailed' | 'duplicateLabels';
type NoticeKey = '' | 'saved' | 'imported' | 'currentLoaded';
type ConflictPrompt = '' | 'load' | 'replace';
const emptySample = (): SampleDraft => ({ labels: [], acknowledged: false, assignee: '', status: '', comment: '' });

function TextField({ label, value, onChange, type = 'text' }: { label: string; value: string | number; onChange: (value: string) => void; type?: 'text' | 'number' }) {
  return <Field label={label}><Input aria-label={label} type={type} value={value} onChange={(event) => onChange(event.currentTarget.value)} /></Field>;
}

function Choice({ label, value, options, onChange }: { label: string; value: string; options: Array<{ value: string; label: string }>; onChange: (value: string) => void }) {
  const choices = options.some((item) => item.value === value) ? options : [...options, { value, label: value }];
  return <Field label={label}><Select aria-label={label} value={value} options={choices} onChange={(option) => onChange(option.value ?? '')} /></Field>;
}

function Check({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return <Checkbox label={label} checked={checked} onChange={(event) => onChange(event.currentTarget.checked)} />;
}

function Requirements({ label, value, onChange, m }: { label: string; value: Requires; onChange: (value: Requires) => void; m: PolicyMessages }) {
  return <fieldset><legend>{label}</legend>{gates.map((gate) => <Check key={gate} label={m.gates[gate]} checked={Boolean(value[gate])} onChange={(checked) => onChange({ ...value, [gate]: checked })} />)}</fieldset>;
}

function LabelRows({ rows, onChange, m }: { rows: LabelRow[]; onChange: (rows: LabelRow[]) => void; m: PolicyMessages }) {
  return <div><p>{m.labels}</p>{rows.map((row, index) => <div key={index}>
    <TextField label={m.labelKey} value={row.key} onChange={(key) => onChange(rows.map((item, i) => i === index ? { ...item, key } : item))} />
    <TextField label={m.labelValue} value={row.value} onChange={(value) => onChange(rows.map((item, i) => i === index ? { ...item, value } : item))} />
    <Button type="button" size="sm" variant="secondary" onClick={() => onChange(rows.filter((_, i) => i !== index))}>{m.remove}</Button>
  </div>)}<Button type="button" size="sm" variant="secondary" onClick={() => onChange([...rows, { key: '', value: '' }])}>{m.addLabel}</Button></div>;
}

function issueArray(error: unknown): PolicyIssue[] {
  const data = (error as { data?: { issues?: unknown } } | null)?.data;
  if (!Array.isArray(data?.issues)) return [];
  return data.issues.filter((item): item is PolicyIssue => item && typeof item === 'object' && typeof item.path === 'string' && typeof item.code === 'string' && typeof item.message === 'string');
}

export function PolicyEditor() {
  const { language, messages } = useI18n();
  const m = messages.policy;
  const styles = useStyles2(getStyles);
  const [core, dispatch] = useReducer(reducePolicyEditor, {});
  const { draft, revision, conflict } = core;
  const [history, setHistory] = useState<PolicyRevision[]>([]);
  const [historyError, setHistoryError] = useState(false);
  const [historyDoc, setHistoryDoc] = useState<{ revision: number; doc: PolicyDoc }>();
  const [samples, setSamples] = useState<SampleDraft[]>([emptySample()]);
  const [preview, setPreview] = useState<PolicyPreview>();
  const [issues, setIssues] = useState<PolicyIssue[]>([]);
  const [notice, setNotice] = useState<NoticeKey>('');
  const [error, setError] = useState<ErrorKey>('');
  const [json, setJSON] = useState('');
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [prompt, setPrompt] = useState<ConflictPrompt>('');

  const refreshHistory = useCallback(async () => {
    try { setHistory(await fetchPolicyRevisions()); setHistoryError(false); }
    catch { setHistoryError(true); }
  }, []);

  useEffect(() => {
    let active = true;
    setError('');
    fetchPolicy().then((response) => {
      if (!active) return;
      dispatch({ type: 'loaded', revision: response.revision, doc: response.doc });
    }).catch(() => { if (active) setError('loadFailed'); });
    void refreshHistory();
    return () => { active = false; };
  }, [attempt, refreshHistory]);

  function edit(change: (value: PolicyDraft) => void) {
    if (!draft) return;
    const next = structuredClone(draft);
    change(next);
    dispatch({ type: 'edited', draft: next });
    setPreview(undefined);
    setIssues([]);
    setNotice('');
  }

  function editSample(index: number, change: (value: SampleDraft) => void) {
    setSamples((previous) => previous.map((item, i) => {
      if (i !== index) return item;
      const next = structuredClone(item); change(next); return next;
    }));
    setPreview(undefined);
  }

  function materialize(): PolicyDoc | undefined {
    if (!draft) return undefined;
    try { return documentFromDraft(draft); }
    catch { setError('duplicateLabels'); return undefined; }
  }

  async function validate() {
    const doc = materialize();
    if (!doc) return;
    let previewSamples: PolicyPreviewSample[];
    try {
      previewSamples = samples.map((sample) => ({ labels: labelsFromRows(sample.labels), acknowledged: sample.acknowledged, assignee: sample.assignee, ...(sample.status ? { status: sample.status } : {}), comment: sample.comment }));
    } catch { setError('duplicateLabels'); return; }
    setBusy(true); setError(''); setNotice(''); setIssues([]);
    try { const result = await previewPolicy(doc, previewSamples); setPreview(result); setIssues(result.issues); }
    catch { setError('requestFailed'); }
    finally { setBusy(false); }
  }

  async function save() {
    const doc = materialize();
    const base = policySaveRevision(core);
    if (!doc || base === undefined) return;
    setBusy(true); setError(''); setNotice(''); setIssues([]);
    try {
      const result = await savePolicy(base, doc);
      dispatch({ type: 'saved', revision: result.revision }); setNotice('saved'); await refreshHistory();
    } catch (requestError) {
      const status = errorStatus(requestError);
      if (status === 400) { setIssues(issueArray(requestError)); setError('validationFailed'); }
      else if (status === 409) {
        const [current] = await Promise.allSettled([fetchPolicy(), refreshHistory()]);
        dispatch({ type: 'rejected', ...(current.status === 'fulfilled' ? { latest: { revision: current.value.revision, doc: current.value.doc } } : {}) });
      } else setError('requestFailed');
    } finally { setBusy(false); }
  }

  async function retryLatest() {
    setBusy(true); setError('');
    try {
      const current = await fetchPolicy();
      dispatch({ type: 'latest', latest: { revision: current.revision, doc: current.doc } });
      await refreshHistory();
    } catch { setError('requestFailed'); }
    finally { setBusy(false); }
  }

  function loadCurrent() {
    if (!conflict?.latest) return;
    dispatch({ type: 'loadCurrent' });
    setPrompt('');
    setPreview(undefined); setIssues([]); setError('');
    setNotice('currentLoaded');
  }

  async function replaceCurrent() {
    const doc = materialize();
    const latest = conflict?.latest;
    if (!doc || !latest) return;
    dispatch({ type: 'confirmReplace' });
    setBusy(true); setError(''); setNotice(''); setIssues([]);
    try {
      const result = await savePolicy(latest.revision, doc);
      dispatch({ type: 'saved', revision: result.revision }); setNotice('saved'); await refreshHistory();
    } catch (requestError) {
      const status = errorStatus(requestError);
      if (status === 400) { setIssues(issueArray(requestError)); setError('validationFailed'); }
      else if (status === 409) {
        const [current] = await Promise.allSettled([fetchPolicy(), refreshHistory()]);
        dispatch({ type: 'rejected', ...(current.status === 'fulfilled' ? { latest: { revision: current.value.revision, doc: current.value.doc } } : {}) });
      } else setError('requestFailed');
    } finally { setBusy(false); }
  }

  async function openRevision(value: number) {
    setBusy(true); setError('');
    try { const result = await fetchPolicyRevision(value); setHistoryDoc({ revision: result.revision, doc: result.doc }); }
    catch { setError('requestFailed'); }
    finally { setBusy(false); }
  }

  function importJSON() {
    try { dispatch({ type: 'edited', draft: draftFromPolicy(parsePolicyJSON(json)) }); setPreview(undefined); setIssues([]); setError(''); setNotice('imported'); }
    catch { setError('importFailed'); }
  }

  function exportJSON() {
    const doc = materialize();
    if (!doc) return;
    const url = URL.createObjectURL(new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' }));
    const link = document.createElement('a'); link.href = url; link.download = 'problem-desk-policy.json'; link.click(); URL.revokeObjectURL(url);
  }

  const selectOptions = (values: string[], none = false) => [...(none ? [{ value: '', label: m.none }] : []), ...values.map((value) => ({ value, label: value }))];
  const roleOptions = [{ value: 'editor', label: m.roleEditor }, { value: 'admin', label: m.roleAdmin }];
  const gateNames = (values: string[]) => values.map((value) => value === 'workflow' ? m.workflow : value === 'role' ? messages.workflow.adminRequired : Object.hasOwn(m.gates, value) ? m.gates[value as keyof Requires] : value).join(', ');
  if (!draft) return <section className={styles.editor}><h2>{m.title}</h2>{error ? <Alert title={m.loadFailed} severity="error">{m[error]}<Button size="sm" variant="secondary" onClick={() => setAttempt((value) => value + 1)}>{messages.common.retry}</Button></Alert> : <p>{messages.common.loading}</p>}</section>;
  const doc = draft.doc;

  return <section className={styles.editor} aria-labelledby="policy-heading">
    <h2 id="policy-heading">{m.title}</h2><p>{m.revision}: {revision ?? '—'}</p>
    {notice && <Alert title={m[notice]} severity="success" />}
    {error && <Alert title={m.requestFailed} severity="error">{m[error]}</Alert>}
    {conflict && <Alert title={m.conflictTitle} severity="warning">
      <p>{m.conflictBody}</p>
      <p>{m.conflictRevisions(conflict.baseRevision, conflict.latest?.revision)}</p>
      {conflict.latest ? <>
        <h4>{m.latestPolicy}</h4>
        <pre className={styles.json}>{JSON.stringify(documentFromDraft(draftFromPolicy(conflict.latest.doc)), null, 2)}</pre>
        <div className={styles.actions}>
          <Button type="button" variant="secondary" disabled={busy} onClick={() => setPrompt('load')}>{m.loadCurrentPolicy}</Button>
          <Button type="button" variant="destructive" disabled={busy} onClick={() => setPrompt('replace')}>{m.replaceCurrentPolicy}</Button>
        </div>
      </> : <>
        <p>{m.conflictUnavailable}</p>
        <Button type="button" variant="secondary" disabled={busy} onClick={() => void retryLatest()}>{messages.common.retry}</Button>
      </>}
    </Alert>}
    {issues.length > 0 && <ul>{issues.map((issue, index) => <li key={index}>{localizePolicyIssue(issue, m.issues, m.issueUnknown)}</li>)}</ul>}
    <fieldset disabled={busy} className={styles.form}>
      <TextField label={m.severityKey} value={doc.severityKey} onChange={(value) => edit((d) => { d.doc.severityKey = value; })} />
      <h3>{m.levels}</h3>
      {doc.levels.map((level, i) => <details key={i} open><summary>{m.levels} {i + 1}: {level.id}</summary>
        <TextField label={m.id} value={level.id} onChange={(value) => edit((d) => { d.doc.levels[i].id = value; })} />
        <TextField label={m.rank} type="number" value={level.rank} onChange={(value) => edit((d) => { d.doc.levels[i].rank = Number(value); })} />
        <Choice label={m.tone} value={level.tone} options={tones.map((value) => ({ value, label: m.tones[value] }))} onChange={(value) => edit((d) => { d.doc.levels[i].tone = value as LevelTone; })} />
        {(['ru', 'en'] as const).map((lang) => <TextField key={lang} label={lang === 'ru' ? m.nameRu : m.nameEn} value={level.name[lang]} onChange={(value) => edit((d) => { d.doc.levels[i].name[lang] = value; })} />)}
        <h4>{m.aliases}</h4>{level.aliases.map((alias, j) => <div key={j}><TextField label={m.alias} value={alias} onChange={(value) => edit((d) => { d.doc.levels[i].aliases[j] = value; })} /><Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.levels[i].aliases.splice(j, 1); })}>{m.remove}</Button></div>)}
        <Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.levels[i].aliases.push(''); })}>{m.addAlias}</Button>{' '}
        <Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.levels.splice(i, 1); })}>{m.remove}</Button>
      </details>)}
      <Button type="button" variant="secondary" onClick={() => edit((d) => { d.doc.levels.push({ id: '', rank: 0, tone: 'grey', name: { ru: '', en: '' }, aliases: [] }); })}>{m.addLevel}</Button>

      <h3>{m.workflows}</h3>
      {doc.workflows.map((workflow, i) => <details key={i} open><summary>{m.workflow} {i + 1}: {workflow.id}</summary>
        <TextField label={m.id} value={workflow.id} onChange={(value) => edit((d) => { d.doc.workflows[i].id = value; })} />
        {(['ru', 'en'] as const).map((lang) => <TextField key={lang} label={lang === 'ru' ? m.nameRu : m.nameEn} value={workflow.name[lang]} onChange={(value) => edit((d) => { d.doc.workflows[i].name[lang] = value; })} />)}
        <Choice label={m.initialState} value={workflow.initialState} options={selectOptions(workflow.states.map((state) => state.id))} onChange={(value) => edit((d) => { d.doc.workflows[i].initialState = value; })} />
        <Choice label={m.minimumRole} value={workflow.minimumRole} options={roleOptions} onChange={(value) => { if (value === 'editor' || value === 'admin') edit((d) => { d.doc.workflows[i].minimumRole = value; }); }} />
        <Requirements label={m.startRequires} value={workflow.startRequires} m={m} onChange={(value) => edit((d) => { d.doc.workflows[i].startRequires = value; })} />
        <h4>{m.states}</h4>{workflow.states.map((state, j) => <div className={styles.item} key={j}>
          <TextField label={m.id} value={state.id} onChange={(value) => edit((d) => { d.doc.workflows[i].states[j].id = value; })} />
          <TextField label={m.rank} type="number" value={state.rank} onChange={(value) => edit((d) => { d.doc.workflows[i].states[j].rank = Number(value); })} />
          {(['ru', 'en'] as const).map((lang) => <TextField key={lang} label={lang === 'ru' ? m.nameRu : m.nameEn} value={state.name[lang]} onChange={(value) => edit((d) => { d.doc.workflows[i].states[j].name[lang] = value; })} />)}
          <Check label={m.operatorComplete} checked={Boolean(state.operatorComplete)} onChange={(value) => edit((d) => { d.doc.workflows[i].states[j].operatorComplete = value; })} />
          <Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.workflows[i].states.splice(j, 1); })}>{m.remove}</Button>
        </div>)}
        <Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.workflows[i].states.push({ id: '', rank: 0, name: { ru: '', en: '' } }); })}>{m.addState}</Button>
        <h4>{m.transitions}</h4>{workflow.transitions.map((transition, j) => <div className={styles.item} key={j}>
          <Choice label={m.from} value={transition.from} options={selectOptions(workflow.states.map((state) => state.id))} onChange={(value) => edit((d) => { d.doc.workflows[i].transitions[j].from = value; })} />
          <Choice label={m.to} value={transition.to} options={selectOptions(workflow.states.map((state) => state.id))} onChange={(value) => edit((d) => { d.doc.workflows[i].transitions[j].to = value; })} />
          <Choice label={m.minimumRole} value={transition.minimumRole} options={roleOptions} onChange={(value) => { if (value === 'editor' || value === 'admin') edit((d) => { d.doc.workflows[i].transitions[j].minimumRole = value; }); }} />
          <Requirements label={m.requires} value={transition.requires} m={m} onChange={(value) => edit((d) => { d.doc.workflows[i].transitions[j].requires = value; })} />
          <Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.workflows[i].transitions.splice(j, 1); })}>{m.remove}</Button>
        </div>)}
        <Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.workflows[i].transitions.push({ from: '', to: '', minimumRole: 'editor', requires: {} }); })}>{m.addTransition}</Button>{' '}
        <Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.workflows.splice(i, 1); })}>{m.remove}</Button>
      </details>)}
      <Button type="button" variant="secondary" onClick={() => edit((d) => { d.doc.workflows.push({ id: '', name: { ru: '', en: '' }, initialState: '', minimumRole: 'editor', startRequires: {}, states: [], transitions: [] }); })}>{m.addWorkflow}</Button>

      <h3>{m.routing}</h3><p>{m.literalHelp}</p>
      {doc.routing.map((route, i) => <div className={styles.item} key={i}>
        <h4>{m.routing} {i + 1}</h4><LabelRows rows={draft.routeLabels[i]} m={m} onChange={(rows) => edit((d) => { d.routeLabels[i] = rows; })} />
        <Choice label={m.optionalLevel} value={route.match.level ?? ''} options={selectOptions(doc.levels.map((level) => level.id), true)} onChange={(value) => edit((d) => { d.doc.routing[i].match.level = value || undefined; })} />
        <Choice label={m.workflow} value={route.workflow} options={selectOptions(doc.workflows.map((workflow) => workflow.id))} onChange={(value) => edit((d) => { d.doc.routing[i].workflow = value; })} />
        <Button type="button" size="sm" variant="secondary" disabled={i === 0} onClick={() => edit((d) => { [d.doc.routing[i - 1], d.doc.routing[i]] = [d.doc.routing[i], d.doc.routing[i - 1]]; [d.routeLabels[i - 1], d.routeLabels[i]] = [d.routeLabels[i], d.routeLabels[i - 1]]; })}>{m.up}</Button>{' '}
        <Button type="button" size="sm" variant="secondary" disabled={i === doc.routing.length - 1} onClick={() => edit((d) => { [d.doc.routing[i + 1], d.doc.routing[i]] = [d.doc.routing[i], d.doc.routing[i + 1]]; [d.routeLabels[i + 1], d.routeLabels[i]] = [d.routeLabels[i], d.routeLabels[i + 1]]; })}>{m.down}</Button>{' '}
        <Button type="button" size="sm" variant="secondary" onClick={() => edit((d) => { d.doc.routing.splice(i, 1); d.routeLabels.splice(i, 1); })}>{m.remove}</Button>
      </div>)}
      <Button type="button" variant="secondary" onClick={() => edit((d) => { d.doc.routing.push({ match: { labels: Object.create(null) }, workflow: '' }); d.routeLabels.push([]); })}>{m.addRoute}</Button>
      <Choice label={m.defaultWorkflow} value={doc.defaultWorkflow ?? ''} options={selectOptions(doc.workflows.map((workflow) => workflow.id), true)} onChange={(value) => edit((d) => { d.doc.defaultWorkflow = value || null; })} />

      <h3>{m.views}</h3>
      {(['recent', 'history'] as const).map((key) => <Choice key={key} label={m[key]} value={doc.views[key]} options={ranges.map((value) => ({ value, label: value }))} onChange={(value) => edit((d) => { d.doc.views[key] = value as PolicyRange; })} />)}
      <fieldset><legend>{m.columns}</legend>{columns.map((column) => <Check key={column} label={m.columnNames[column]} checked={doc.views.columns.includes(column)} onChange={(checked) => edit((d) => { d.doc.views.columns = checked ? [...d.doc.views.columns, column] : d.doc.views.columns.filter((item) => item !== column); })} />)}</fieldset>
      <TextField label={m.recoveredTrust} type="number" value={doc.source.recoveredTrustMinutes} onChange={(value) => edit((d) => { d.doc.source.recoveredTrustMinutes = Number(value); })} /><p>{m.sourceHelp}</p>

      <h3>{m.samples}</h3>{samples.map((sample, i) => <div className={styles.item} key={i}>
        <h4>{m.samples} {i + 1}</h4><LabelRows m={m} rows={sample.labels} onChange={(rows) => editSample(i, (item) => { item.labels = rows; })} />
        <Check label={m.acknowledged} checked={sample.acknowledged} onChange={(value) => editSample(i, (item) => { item.acknowledged = value; })} />
        <TextField label={m.assignee} value={sample.assignee} onChange={(value) => editSample(i, (item) => { item.assignee = value; })} />
        <Choice label={m.sourceStatus} value={sample.status} options={[{ value: '', label: m.none }, { value: 'active', label: messages.status.active }, { value: 'recovered', label: messages.status.recovered }]} onChange={(value) => editSample(i, (item) => { item.status = value as SampleDraft['status']; })} />
        <TextField label={m.comment} value={sample.comment} onChange={(value) => editSample(i, (item) => { item.comment = value; })} />
        <Button type="button" size="sm" variant="secondary" onClick={() => { setSamples((previous) => previous.filter((_, index) => index !== i)); setPreview(undefined); }}>{m.remove}</Button>
      </div>)}
      <Button type="button" variant="secondary" onClick={() => { setSamples((previous) => [...previous, emptySample()]); setPreview(undefined); }}>{m.addSample}</Button>
      <div className={styles.actions}><Button type="button" variant="secondary" onClick={() => void validate()}>{m.validatePreview}</Button><Button type="button" disabled={busy || policySaveRevision(core) === undefined} onClick={() => void save()}>{m.save}</Button>{busy && m.busy}</div>
    </fieldset>
    {preview && <section><h3>{m.result}</h3><p>{preview.valid ? m.validationOk : m.validationFailed}</p>
      {preview.occupancyViolations.map((item, i) => {
        const workflowIndex = doc.workflows.findIndex((workflow) => workflow.id === item.workflowId);
        const path = workflowIndex < 0 ? '$.workflows' : `$.workflows[${workflowIndex}].states`;
        return <p key={i}>{path}: {m.occupancy}: {item.workflowId} / {item.stateId} ({item.count})</p>;
      })}
      {preview.samples.map((sample, i) => <div className={styles.item} key={i}><strong>{m.samples} {i + 1}</strong><p>{m.levels}: {doc.levels.find((level) => level.id === sample.levelId)?.name[language] ?? sample.levelId} ({sample.levelId})</p><p>{m.matched}: {sample.levelMatched ? m.yes : m.no}</p><p>{m.workflow}: {doc.workflows.find((workflow) => workflow.id === sample.workflowId)?.name[language] || sample.workflowId || m.none}</p><p>{m.canStart}: {sample.canStart ? m.yes : m.no}</p><p>{m.missing}: {gateNames(sample.missingStartRequires) || m.none}</p></div>)}
    </section>}
    <section><h3>{m.historyTitle}</h3>{historyError && <Alert title={m.historyFailed} severity="warning"><Button type="button" variant="secondary" size="sm" onClick={() => void refreshHistory()}>{messages.common.retry}</Button></Alert>}
      <ul>{history.map((entry) => <li key={entry.revision}>{entry.revision} · {entry.createdAt} · {entry.createdBy} · {entry.action} <Button type="button" size="sm" variant="secondary" disabled={busy} onClick={() => void openRevision(entry.revision)}>{m.openRevision}</Button></li>)}</ul>
      {historyDoc && <div><h4>{m.revisionPreview}: {historyDoc.revision}</h4><pre className={styles.json}>{JSON.stringify(documentFromDraft(draftFromPolicy(historyDoc.doc)), null, 2)}</pre><Button type="button" variant="secondary" disabled={busy} onClick={() => { dispatch({ type: 'edited', draft: draftFromPolicy(historyDoc.doc) }); setPreview(undefined); setIssues([]); setError(''); setNotice('imported'); }}>{m.loadDraft}</Button></div>}
    </section>
    <section><h3>{m.json}</h3><p>{m.jsonHelp}</p><TextArea aria-label={m.json} rows={8} value={json} onChange={(event) => setJSON(event.currentTarget.value)} /><div className={styles.actions}><Button type="button" variant="secondary" disabled={busy} onClick={importJSON}>{m.import}</Button><Button type="button" variant="secondary" disabled={busy} onClick={exportJSON}>{m.export}</Button></div></section>
    <ConfirmModal isOpen={prompt === 'load'} title={m.confirmLoadTitle} body={m.confirmLoadBody} confirmText={m.loadCurrentPolicy} onConfirm={loadCurrent} onDismiss={() => setPrompt('')} />
    <ConfirmModal isOpen={prompt === 'replace'} title={m.confirmReplaceTitle} body={m.confirmReplaceBody} confirmText={m.replaceCurrentPolicy} confirmVariant="destructive" onConfirm={() => { setPrompt(''); void replaceCurrent(); }} onDismiss={() => setPrompt('')} />
  </section>;
}

const getStyles = (theme: GrafanaTheme2) => ({
  editor: css`margin-top: ${theme.spacing(3)}; padding: ${theme.spacing(2)}; border: 1px solid ${theme.colors.border.weak}; border-radius: ${theme.shape.radius.default};`,
  form: css`border: 0; padding: 0; min-width: 0; display: flex; flex-direction: column; gap: ${theme.spacing(1)};`,
  item: css`padding: ${theme.spacing(1)}; margin: ${theme.spacing(1)} 0; border: 1px solid ${theme.colors.border.weak};`,
  actions: css`display: flex; flex-wrap: wrap; gap: ${theme.spacing(1)}; margin: ${theme.spacing(1)} 0;`,
  json: css`white-space: pre-wrap; overflow-wrap: anywhere; max-height: 30rem; overflow: auto;`,
});
