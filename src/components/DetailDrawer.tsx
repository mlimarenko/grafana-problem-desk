import React, { useState } from 'react';
import { css } from '@emotion/css';
import type { GrafanaTheme2 } from '@grafana/data';
import { Alert, Button, Drawer, LinkButton, LoadingPlaceholder, Stack, TextArea, useStyles2 } from '@grafana/ui';
import { errorText, useI18n } from '../i18n';
import type { ViewPreferences } from '../lib/preferences';
import {
  activityContent, elapsedMs, isAcknowledged, levelDisplay, mergedActivity, problemTitle, safeURL, targetName,
  type Detail, type ErrorKind, type Level, type PolicyDoc,
} from '../model';
import { ProblemStatus, SeverityBadge } from './ProblemIndicators';
import { ViewOptions } from './ViewOptions';
import { WorkflowSection } from './WorkflowSection';

export type BusyAction = 'ack' | 'unack' | 'owner' | 'comment';

interface Props {
  selectedId: string;
  detail?: Detail;
  loading: boolean;
  error?: ErrorKind;
  actionError?: ErrorKind;
  busyAction?: BusyAction;
  draft: string;
  canWrite: boolean;
  currentLogin: string;
  ownerConflict: boolean;
  levels: Level[];
  severityKey: string;
  policy?: PolicyDoc;
  preferences: ViewPreferences;
  onWorkflowChanged: () => void;
  onDraftChange: (value: string) => void;
  onClose: () => void;
  onRetry: () => void;
  onAck: () => void;
  onUnack: () => void;
  onClaim: () => void;
  onRelease: () => void;
  onComment: () => void;
}

export function DetailDrawer(props: Props) {
  return props.selectedId ? <EpisodeDrawer key={props.selectedId} {...props} /> : null;
}

function EpisodeDrawer({
  selectedId, detail, loading, error, actionError, busyAction, draft, canWrite, currentLogin, ownerConflict,
  levels, severityKey, policy, preferences, onWorkflowChanged, onDraftChange, onClose, onRetry,
  onAck, onUnack, onClaim, onRelease, onComment,
}: Props) {
  const styles = useStyles2(getStyles);
  const { messages: m, language, formatDate, formatDuration } = useI18n();
  const [copyResult, setCopyResult] = useState<'idle' | 'copied' | 'failed'>('idle');
  const problem = detail?.problem.id === selectedId ? detail.problem : undefined;
  const title = problem ? problemTitle(problem) || m.table.untitled : m.drawer.title;
  const host = problem && targetName(problem);
  const showDetails = preferences.visible.has('details');
  const showActions = preferences.visible.has('actions');
  const severity = problem && showDetails && levelDisplay(problem, levels, severityKey, language);
  const duration = problem && showDetails ? elapsedMs(problem, Date.now()) : undefined;
  const rule = problem && safeURL(problem.generatorURL);
  const acknowledged = problem && isAcknowledged(problem);
  const activity = problem && detail ? mergedActivity(detail).filter((item) => showActions || item.kind === 'comment') : [];
  const description = problem?.annotations.description?.trim();
  const canChangeOwner = canWrite && currentLogin && (!problem?.assignee || problem.assignee === currentLogin);

  async function copyLink() {
    const url = new URL(window.location.href);
    for (const key of ['fingerprint', 'startsAt', 'matchers', 'ruleUID', 'instance', 'device', 'mountpoint', 'name', 'datname']) {
      url.searchParams.delete(key);
    }
    url.searchParams.set('problem', selectedId);
    try {
      await navigator.clipboard.writeText(url.toString());
      setCopyResult('copied');
    } catch {
      setCopyResult('failed');
    }
  }

  const heading = <div className={styles.heading}>
    <h2>{title}</h2>
    {problem && <div className={styles.row}>
      {host && <span className={styles.caption}>{host}</span>}
      <ProblemStatus status={problem.status} />
    </div>}
  </div>;

  return <Drawer title={heading} onClose={onClose} size="sm" width="36rem">
    <div className={styles.body} data-testid="problem-detail" aria-busy={loading || Boolean(busyAction)}>
      {error && <Alert title={m.errors.detailTitle} severity="error">
        {errorText(error, m)} <Button variant="secondary" size="sm" onClick={onRetry}>{m.common.retry}</Button>
      </Alert>}
      {actionError && <Alert title={m.errors.actionTitle} severity="error">{errorText(actionError, m)}</Alert>}
      {!problem && !error && <LoadingPlaceholder text={m.common.loading} />}
      {problem && <>
        {description && description !== title && <p className={styles.prose}>{description}</p>}
        <div className={styles.row}>
          {problem.assignee
            ? <span><span className={styles.caption}>{m.table.owner}: </span><strong>{problem.assignee}</strong></span>
            : !canChangeOwner && <span className={styles.caption}>{m.drawer.free}</span>}
          {canChangeOwner && (
            <Button variant={problem.assignee ? 'secondary' : 'primary'} size="sm" disabled={Boolean(busyAction)}
              onClick={problem.assignee ? onRelease : onClaim}>
              {problem.assignee ? m.drawer.release : m.drawer.claim}
            </Button>
          )}
          {acknowledged ? (
            <span className={styles.row}>
              <span className={styles.caption}>{m.ack.acknowledged}</span>
              {canWrite && <Button type="button" size="sm" variant="secondary" fill="text" aria-label={m.drawer.unacknowledge}
                disabled={Boolean(busyAction)} onClick={onUnack}>{m.drawer.undo}</Button>}
            </span>
          ) : canWrite && (
            <Button type="button" variant="secondary" size="sm" disabled={Boolean(busyAction)} onClick={onAck}>{m.drawer.acknowledge}</Button>
          )}
          <span className={styles.more}>
            <ViewOptions scope="drawer" preferences={preferences}>
              <Stack direction="column" alignItems="start" gap={0.5}>
                {rule && <LinkButton variant="secondary" fill="text" size="sm" icon="external-link-alt" href={rule} target="_blank" rel="noopener noreferrer">{m.drawer.sourceRule}</LinkButton>}
                <Button type="button" variant="secondary" fill="text" size="sm" icon="link" onClick={() => void copyLink()}>
                  <span aria-live="polite">{copyResult === 'copied' ? m.drawer.copyDone : copyResult === 'failed' ? m.drawer.copyFailed : m.drawer.copyLink}</span>
                </Button>
              </Stack>
            </ViewOptions>
          </span>
        </div>
        {ownerConflict && <p className={styles.warning} role="status">{m.drawer.ownerConflict}</p>}
        {preferences.visible.has('workflow') && policy && (
          <WorkflowSection problem={problem} policy={policy} canWrite={canWrite} onChanged={onWorkflowChanged} />
        )}
        {(canWrite || activity.length > 0) && <section className={styles.history} aria-labelledby="episode-history-heading">
          <h3 id="episode-history-heading">{showActions ? m.drawer.activity : m.drawer.comments}</h3>
          {canWrite && <form onSubmit={(event) => { event.preventDefault(); if (draft.trim() && !busyAction) onComment(); }}>
            <TextArea aria-label={m.activity.comment} rows={2} maxLength={5000} value={draft} placeholder={m.drawer.commentPlaceholder}
              onChange={(event) => onDraftChange(event.currentTarget.value)} />
            {draft.length > 0 && <div className={styles.commentFooter}>
              {draft.length >= 4500 && <span className={styles.caption}>{draft.length} / 5000</span>}
              <Button type="submit" size="sm" disabled={Boolean(busyAction) || !draft.trim()}>{m.drawer.addComment}</Button>
            </div>}
          </form>}
          {activity.length > 0 && <ol>
            {activity.map((item) => {
              const { text, body } = activityContent(item, language, policy?.workflows);
              return <li key={item.id}>
                <div className={styles.activityLine}>
                  <strong>{item.author || m.common.unknown}</strong>
                  {text}
                  <time dateTime={item.at}>{formatDate(item.at)}</time>
                </div>
                {body && <p className={styles.prose}>{body}</p>}
              </li>;
            })}
          </ol>}
        </section>}
        {showDetails && <details className={styles.disclosure}>
          <summary>{m.view.details}</summary>
          <div className={styles.raw}>
            <dl>
              {severity && <><dt>{m.table.severity}</dt><dd><SeverityBadge {...severity} /></dd></>}
              <dt>{m.drawer.facts.started}</dt><dd><time dateTime={problem.startsAt}>{formatDate(problem.startsAt)}</time></dd>
              {problem.endsAt && <><dt>{m.drawer.facts.recovered}</dt><dd><time dateTime={problem.endsAt}>{formatDate(problem.endsAt)}</time></dd></>}
              {duration !== undefined && <><dt>{m.drawer.facts.duration}</dt><dd>{formatDuration(duration)}</dd></>}
              {acknowledged && <>
                <dt>{m.table.ack}</dt><dd>{problem.acknowledgedBy}
                  {problem.acknowledgedAt && <> · <time dateTime={problem.acknowledgedAt}>{formatDate(problem.acknowledgedAt)}</time></>}
                </dd>
              </>}
              <dt>{m.drawer.facts.episode}</dt><dd><code>{problem.id}</code></dd>
            </dl>
            {[{ label: m.drawer.labels, data: problem.labels }, { label: m.drawer.annotations, data: problem.annotations }].map(({ label, data }) => {
              if (!Object.keys(data).length) return null;
              return <React.Fragment key={label}>
                <h4>{label}</h4>
                <dl>{Object.entries(data).sort(([left], [right]) => left.localeCompare(right)).map(([key, value]) =>
                  <React.Fragment key={key}><dt>{key}</dt><dd>{value}</dd></React.Fragment>)}</dl>
              </React.Fragment>;
            })}
          </div>
        </details>}
      </>}
    </div>
  </Drawer>;
}

const getStyles = (theme: GrafanaTheme2) => ({
  heading: css({
    paddingRight: theme.spacing(4), overflowWrap: 'anywhere',
    h2: { margin: theme.spacing(0, 0, 1), fontSize: theme.typography.h4.fontSize, lineHeight: theme.typography.h4.lineHeight },
  }),
  body: css({ display: 'flex', flexDirection: 'column', gap: theme.spacing(2), minWidth: 0, paddingBottom: theme.spacing(2) }),
  row: css({ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: theme.spacing(1), overflowWrap: 'anywhere' }),
  more: css({ marginInlineStart: 'auto' }),
  warning: css({ margin: 0, color: theme.colors.warning.text }),
  caption: css({ color: theme.colors.text.secondary }),
  prose: css({ margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }),
  history: css({
    paddingTop: theme.spacing(2), borderTop: `1px solid ${theme.colors.border.weak}`,
    h3: { fontSize: theme.typography.h5.fontSize, margin: theme.spacing(0, 0, 1) },
    ol: { listStyle: 'none', padding: 0, margin: 0 },
    li: { paddingTop: theme.spacing(1.5) },
    'li p': { marginTop: theme.spacing(0.5) },
  }),
  commentFooter: css({ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', gap: theme.spacing(1), marginTop: theme.spacing(1) }),
  activityLine: css({
    display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: theme.spacing(0.75), overflowWrap: 'anywhere',
    time: { color: theme.colors.text.secondary, fontSize: theme.typography.bodySmall.fontSize },
  }),
  disclosure: css({ borderTop: `1px solid ${theme.colors.border.weak}`, paddingTop: theme.spacing(1.5), summary: { cursor: 'pointer', color: theme.colors.text.secondary } }),
  raw: css({
    h4: { fontSize: theme.typography.body.fontSize, margin: theme.spacing(1.5, 0, 0.5) },
    dl: { display: 'grid', gridTemplateColumns: 'minmax(5rem, 1fr) minmax(0, 2fr)', gap: theme.spacing(0.5, 1), margin: theme.spacing(1, 0), overflowWrap: 'anywhere' },
    dt: { color: theme.colors.text.secondary, fontWeight: 'normal' },
    dd: { margin: 0 },
  }),
});
