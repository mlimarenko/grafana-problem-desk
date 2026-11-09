import React, { useEffect, useRef, useState } from 'react';
import { css } from '@emotion/css';
import type { GrafanaTheme2 } from '@grafana/data';
import { Alert, Button, Icon, IconButton, Input, LoadingPlaceholder, TextArea, useStyles2 } from '@grafana/ui';
import {
  assignmentTarget,
  elapsedMs,
  isAcknowledged,
  mergedActivity,
  problemTitle,
  safeURL,
  severityTone,
  targetMetadata,
  targetName,
  type ActivityItem,
  type Detail,
  type ErrorKind,
  type Problem,
} from '../model';
import { errorText, useI18n } from '../i18n';

export type BusyAction = 'ack' | 'unack' | 'assign' | 'comment';

interface Props {
  selectedId: string;
  detail?: Detail;
  loading: boolean;
  error?: ErrorKind;
  actionError?: ErrorKind;
  busyAction?: BusyAction;
  draft: string;
  ownerDraft: string;
  canWrite: boolean;
  onDraftChange: (value: string) => void;
  onOwnerDraftChange: (value: string) => void;
  onClose: () => void;
  onRetry: () => void;
  onAck: () => void;
  onUnack: () => void;
  onAssign: () => void;
  onComment: () => void;
}

export function DetailDrawer({
  selectedId,
  detail,
  loading,
  error,
  actionError,
  busyAction,
  draft,
  ownerDraft,
  canWrite,
  onDraftChange,
  onOwnerDraftChange,
  onClose,
  onRetry,
  onAck,
  onUnack,
  onAssign,
  onComment,
}: Props) {
  const styles = useStyles2(getStyles);
  const { messages: m, formatDate, formatDuration, formatRelative } = useI18n();
  const panelRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const [copyResult, setCopyResult] = useState<'idle' | 'copied' | 'failed'>('idle');

  useEffect(() => {
    setCopyResult('idle');
  }, [selectedId]);

  useEffect(() => {
    if (!selectedId) {
      return;
    }
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    closeRef.current?.focus();

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onCloseRef.current();
        return;
      }
      if (event.key !== 'Tab' || !panelRef.current) {
        return;
      }
      const focusable = Array.from(
        panelRef.current.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), summary, [tabindex]:not([tabindex="-1"])'
        )
      ).filter((element) => element.offsetParent !== null);
      if (focusable.length === 0) {
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = previousOverflow;
      previous?.focus?.();
    };
  }, [selectedId]);

  if (!selectedId) {
    return null;
  }

  const currentDetail = detail?.problem?.id === selectedId ? detail : undefined;
  const problem = currentDetail?.problem;
  const activity = currentDetail ? mergedActivity(currentDetail) : [];
  const generatorURL = problem ? safeURL(problem.generatorURL) : undefined;
  const acknowledged = problem ? isAcknowledged(problem) : false;
  const duration = problem ? elapsedMs(problem, Date.now()) : undefined;
  const severity = problem ? severityTone(problem.labels.severity) : 'unknown';
  const title = problem ? problemTitle(problem) || m.table.untitled : m.drawer.title;

  function activityText(item: ActivityItem): string {
    if (item.eventKind === 'recovered') {
      return m.status.recovered;
    }
    switch (item.kind) {
      case 'ack':
        return m.activity.acknowledged;
      case 'unack':
        return m.activity.unacknowledged;
      case 'assign': {
        const target = assignmentTarget(item.body ?? '');
        return target ? m.activity.ownerSet(target) : m.activity.ownerCleared;
      }
      case 'comment':
        return m.activity.comment;
      default:
        return m.activity.unknownKind(item.eventKind || m.common.unknown);
    }
  }

  function activityIcon(item: ActivityItem) {
    switch (item.kind) {
      case 'comment':
        return 'comment-alt' as const;
      case 'assign':
      case 'unack':
        return 'user' as const;
      case 'ack':
        return 'check' as const;
      default:
        return 'info-circle' as const;
    }
  }

  async function copyAppLink() {
    if (!problem) {
      return;
    }
    try {
      if (!navigator.clipboard) {
        throw new Error('clipboard unavailable');
      }
      const url = new URL(window.location.href);
      for (const key of [
        'fingerprint',
        'startsAt',
        'matchers',
        'ruleUID',
        'instance',
        'device',
        'mountpoint',
        'name',
        'datname',
      ]) {
        url.searchParams.delete(key);
      }
      url.searchParams.set('problem', problem.id);
      await navigator.clipboard.writeText(url.toString());
      setCopyResult('copied');
    } catch {
      setCopyResult('failed');
    }
  }

  return (
    <>
      <div className={styles.backdrop} onClick={onClose} aria-hidden="true" />
      <aside
        ref={panelRef}
        className={styles.drawer}
        role="dialog"
        aria-modal="true"
        aria-labelledby="problem-detail-title"
        aria-busy={loading || Boolean(busyAction)}
        data-testid="problem-detail"
      >
        <header className={styles.header}>
          <div className={styles.headerText}>
            <div className={styles.eyebrow}>{m.drawer.title}</div>
            <h2 id="problem-detail-title" className={styles.title}>
              {title}
            </h2>
            {problem && <div className={styles.target}>{targetName(problem) || m.table.noTarget}</div>}
          </div>
          <IconButton ref={closeRef} name="times" aria-label={m.drawer.close} onClick={onClose} />
        </header>

        <div className={styles.content}>
          {error && (
            <Alert title={m.errors.detailTitle} severity="error">
              <p>{errorText(error, m)}</p>
              <Button variant="secondary" size="sm" onClick={onRetry}>
                {m.common.retry}
              </Button>
            </Alert>
          )}
          {actionError && (
            <Alert title={m.errors.actionTitle} severity="error">
              {errorText(actionError, m)}
            </Alert>
          )}
          {!problem && !error && <LoadingPlaceholder text={loading ? m.common.loading : m.errors.detailTitle} />}
          {problem && (
            <>
              <div className={styles.badges}>
                <span className={styles.severityBadge(severity)}>
                  <Icon name="exclamation-circle" size="sm" />
                  {m.severity[severity]}
                </span>
                <span className={styles.statusBadge(problem.status)}>
                  <span className={styles.statusDot(problem.status)} />
                  {problem.status === 'active' ? m.status.active : m.status.recovered}
                </span>
                {acknowledged && (
                  <span className={styles.ackBadge}>
                    <Icon name="check" size="sm" />
                    {m.ack.acknowledgedBy(problem.acknowledgedBy)}
                  </span>
                )}
              </div>

              <dl className={styles.facts}>
                <dt>{m.drawer.facts.state}</dt>
                <dd>{problem.status === 'active' ? m.status.active : m.status.recovered}</dd>
                <dt>{m.drawer.facts.started}</dt>
                <dd>{formatDate(problem.startsAt)}</dd>
                <dt>{m.drawer.facts.recovered}</dt>
                <dd>{problem.endsAt ? formatDate(problem.endsAt) : '—'}</dd>
                <dt>{m.drawer.facts.duration}</dt>
                <dd>{duration === undefined ? '—' : formatDuration(duration)}</dd>
                <dt>{m.drawer.facts.owner}</dt>
                <dd>{problem.assignee || m.filters.unassigned}</dd>
                <dt>{m.drawer.facts.ack}</dt>
                <dd>{acknowledged ? m.ack.acknowledgedBy(problem.acknowledgedBy) : m.ack.notAcknowledged}</dd>
                <dt>{m.drawer.facts.ackAt}</dt>
                <dd>{problem.acknowledgedAt ? formatDate(problem.acknowledgedAt) : '—'}</dd>
                <dt>{m.drawer.facts.episode}</dt>
                <dd>
                  <code className={styles.episodeId}>{problem.id}</code>
                </dd>
              </dl>

              <div className={styles.companyActions}>
                <div className={styles.linkActions}>
                  {generatorURL && (
                    <a className={styles.externalLink} href={generatorURL} target="_blank" rel="noopener noreferrer">
                      <Icon name="external-link-alt" size="sm" />
                      {m.drawer.sourceRule}
                    </a>
                  )}
                  <Button
                    variant="secondary"
                    size="sm"
                    icon="copy"
                    onClick={() => void copyAppLink()}
                  >
                    {copyResult === 'copied'
                      ? m.drawer.copyDone
                      : copyResult === 'failed'
                        ? m.drawer.copyFailed
                        : m.drawer.copyLink}
                  </Button>
                </div>
                {canWrite && (
                  <Button
                    variant={acknowledged ? 'secondary' : 'primary'}
                    size="sm"
                    icon={acknowledged ? 'times' : 'check'}
                    disabled={Boolean(busyAction)}
                    onClick={acknowledged ? onUnack : onAck}
                  >
                    {acknowledged ? m.drawer.unacknowledge : m.drawer.acknowledge}
                  </Button>
                )}
              </div>
              <p className={styles.hint}>{m.drawer.ackHint}</p>
              {!canWrite && <p className={styles.hint}>{m.drawer.viewer}</p>}

              {problem.annotations.description && <p className={styles.prose}>{problem.annotations.description}</p>}

              {canWrite && (
                <form
                  className={styles.inlineForm}
                  onSubmit={(event) => {
                    event.preventDefault();
                    onAssign();
                  }}
                >
                  <label className={styles.fieldLabel} htmlFor="problem-owner">
                    {m.drawer.owner}
                  </label>
                  <div className={styles.inlineRow}>
                    <Input
                      id="problem-owner"
                      name="assignee"
                      value={ownerDraft}
                      maxLength={255}
                      placeholder={m.drawer.ownerPlaceholder}
                      onChange={(event) => onOwnerDraftChange(event.currentTarget.value)}
                    />
                    <Button
                      type="submit"
                      variant="secondary"
                      size="sm"
                      disabled={Boolean(busyAction) || ownerDraft.trim() === (problem.assignee ?? '')}
                    >
                      {busyAction === 'assign' ? m.drawer.saving : m.drawer.saveOwner}
                    </Button>
                  </div>
                  <p className={styles.hint}>{m.drawer.ownerHint}</p>
                </form>
              )}

              <section className={styles.section} aria-labelledby="problem-comments-heading">
                <h3 id="problem-comments-heading" className={styles.sectionTitle}>
                  {m.drawer.comments}
                </h3>
                {canWrite ? (
                  <form
                    onSubmit={(event) => {
                      event.preventDefault();
                      onComment();
                    }}
                  >
                    <TextArea
                      aria-label={m.drawer.comments}
                      rows={4}
                      maxLength={5000}
                      value={draft}
                      placeholder={m.drawer.commentPlaceholder}
                      onChange={(event) => onDraftChange(event.currentTarget.value)}
                    />
                    <div className={styles.commentFooter}>
                      <span className={styles.hint}>{m.drawer.commentLimit(draft.length, 5000)}</span>
                      <Button type="submit" size="sm" disabled={Boolean(busyAction) || !draft.trim()}>
                        {busyAction === 'comment' ? m.drawer.saving : m.drawer.addComment}
                      </Button>
                    </div>
                  </form>
                ) : (
                  <p className={styles.hint}>{m.drawer.viewer}</p>
                )}
              </section>

              <section className={styles.section} aria-labelledby="problem-activity-heading">
                <div className={styles.sectionHeader}>
                  <h3 id="problem-activity-heading" className={styles.sectionTitle}>
                    {m.drawer.activity}
                  </h3>
                  <span className={styles.hint}>{m.drawer.activityNewest}</span>
                </div>
                {activity.length === 0 && <p className={styles.hint}>{m.drawer.noActivity}</p>}
                <ol className={styles.activity}>
                  {activity.map((item) => (
                    <li key={item.id} className={styles.activityItem}>
                      <span className={styles.activityIcon}>
                        <Icon name={activityIcon(item)} size="sm" />
                      </span>
                      <div className={styles.activityBody}>
                        <div className={styles.activityMeta}>
                          <strong>{item.author || m.common.unknown}</strong>
                          <span>·</span>
                          <span>{activityText(item)}</span>
                          <span>·</span>
                          <time dateTime={item.at}>{formatDate(item.at)}</time>
                          <span>({formatRelative(item.at)})</span>
                        </div>
                        {item.body && item.kind !== 'ack' && item.kind !== 'unack' && item.kind !== 'assign' && (
                          <p className={styles.prose}>{item.body}</p>
                        )}
                      </div>
                    </li>
                  ))}
                </ol>
              </section>

              <details className={styles.disclosure}>
                <summary>
                  {m.drawer.labels} ({Object.keys(problem.labels).length})
                </summary>
                {Object.keys(problem.labels).length === 0 ? (
                  <p className={styles.hint}>{m.drawer.noLabels}</p>
                ) : (
                  <dl className={styles.labelGrid}>
                    {Object.entries(problem.labels)
                      .sort(([left], [right]) => left.localeCompare(right))
                      .map(([key, value]) => (
                        <React.Fragment key={key}>
                          <dt>{key}</dt>
                          <dd>{value}</dd>
                        </React.Fragment>
                      ))}
                  </dl>
                )}
              </details>

              <details className={styles.disclosure}>
                <summary>
                  {m.drawer.annotations} ({Object.keys(problem.annotations).length})
                </summary>
                {Object.keys(problem.annotations).length === 0 ? (
                  <p className={styles.hint}>{m.drawer.noAnnotations}</p>
                ) : (
                  <dl className={styles.labelGrid}>
                    {Object.entries(problem.annotations)
                      .sort(([left], [right]) => left.localeCompare(right))
                      .map(([key, value]) => (
                        <React.Fragment key={key}>
                          <dt>{key}</dt>
                          <dd>{value}</dd>
                        </React.Fragment>
                      ))}
                  </dl>
                )}
              </details>
            </>
          )}
        </div>
      </aside>
    </>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  backdrop: css`
    position: fixed;
    inset: 0;
    z-index: ${theme.zIndex.modal};
    background: ${theme.components.overlay.background};
    animation: fadeIn 120ms ease-out;
    @keyframes fadeIn {
      from {
        opacity: 0;
      }
      to {
        opacity: 1;
      }
    }
  `,
  drawer: css`
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    z-index: calc(${theme.zIndex.modal} + 1);
    display: flex;
    flex-direction: column;
    width: min(480px, 100vw);
    max-width: 100%;
    background: ${theme.colors.background.canvas};
    border-left: 1px solid ${theme.colors.border.medium};
    box-shadow: ${theme.shadows.z3};
    animation: slideIn 160ms ease-out;
    @keyframes slideIn {
      from {
        transform: translateX(100%);
      }
      to {
        transform: translateX(0);
      }
    }
    @media (max-width: 640px) {
      width: 100%;
      border-left: 0;
    }
    @media (prefers-reduced-motion: reduce) {
      animation: none;
    }
  `,
  header: css`
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: ${theme.spacing(2)};
    padding: ${theme.spacing(2)};
    border-bottom: 1px solid ${theme.colors.border.weak};
    background: ${theme.colors.background.canvas};
  `,
  headerText: css`
    min-width: 0;
  `,
  eyebrow: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    text-transform: uppercase;
    letter-spacing: 0.04em;
  `,
  title: css`
    margin: ${theme.spacing(0.5)} 0 0;
    font-size: ${theme.typography.h4.fontSize};
    line-height: ${theme.typography.h4.lineHeight};
    overflow-wrap: anywhere;
  `,
  target: css`
    margin-top: ${theme.spacing(0.5)};
    color: ${theme.colors.text.secondary};
    overflow-wrap: anywhere;
  `,
  content: css`
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: ${theme.spacing(2)};
  `,
  badges: css`
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: ${theme.spacing(1)};
    margin-bottom: ${theme.spacing(2)};
  `,
  severityBadge: (tone: ReturnType<typeof severityTone>) => css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    padding: 2px ${theme.spacing(1)};
    border: 1px solid ${tone === 'critical' ? theme.colors.error.border : tone === 'warning' ? theme.colors.warning.border : theme.colors.info.border};
    border-radius: 999px;
    color: ${tone === 'critical' ? theme.colors.error.text : tone === 'warning' ? theme.colors.warning.text : theme.colors.info.text};
  `,
  statusBadge: (status: Problem['status']) => css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    color: ${status === 'active' ? theme.colors.error.text : theme.colors.success.text};
    font-weight: ${theme.typography.fontWeightMedium};
  `,
  statusDot: (status: Problem['status']) => css`
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: ${status === 'active' ? theme.colors.error.main : theme.colors.success.main};
  `,
  ackBadge: css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    color: ${theme.colors.success.text};
    overflow-wrap: anywhere;
  `,
  facts: css`
    display: grid;
    grid-template-columns: minmax(100px, 140px) minmax(0, 1fr);
    gap: ${theme.spacing(1)} ${theme.spacing(1.5)};
    margin: 0 0 ${theme.spacing(2)};
    font-size: ${theme.typography.bodySmall.fontSize};
    dt {
      color: ${theme.colors.text.secondary};
    }
    dd {
      margin: 0;
      overflow-wrap: anywhere;
    }
  `,
  episodeId: css`
    font-family: ${theme.typography.fontFamilyMonospace};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
  companyActions: css`
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: ${theme.spacing(1)};
    margin-bottom: ${theme.spacing(1)};
  `,
  linkActions: css`
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: ${theme.spacing(1)};
  `,
  externalLink: css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    color: ${theme.colors.text.link};
  `,
  hint: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    margin: ${theme.spacing(0.5)} 0;
  `,
  prose: css`
    margin: ${theme.spacing(1)} 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  `,
  inlineForm: css`
    margin: ${theme.spacing(2)} 0;
    padding: ${theme.spacing(1.5)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
  `,
  fieldLabel: css`
    display: block;
    margin-bottom: ${theme.spacing(0.5)};
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
  inlineRow: css`
    display: flex;
    gap: ${theme.spacing(1)};
    align-items: flex-start;
    > :first-child {
      flex: 1;
      min-width: 0;
    }
  `,
  section: css`
    margin-top: ${theme.spacing(3)};
  `,
  sectionHeader: css`
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: ${theme.spacing(1)};
  `,
  sectionTitle: css`
    margin: 0 0 ${theme.spacing(1)};
    font-size: ${theme.typography.h5.fontSize};
  `,
  commentFooter: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: ${theme.spacing(1)};
    margin-top: ${theme.spacing(0.5)};
  `,
  activity: css`
    list-style: none;
    margin: ${theme.spacing(1)} 0 0;
    padding: 0;
  `,
  activityItem: css`
    display: flex;
    gap: ${theme.spacing(1)};
    padding: ${theme.spacing(1)} 0;
    border-bottom: 1px solid ${theme.colors.border.weak};
    &:last-child {
      border-bottom: 0;
    }
  `,
  activityIcon: css`
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    background: ${theme.colors.background.secondary};
    color: ${theme.colors.text.secondary};
    flex: 0 0 auto;
  `,
  activityBody: css`
    min-width: 0;
    flex: 1;
  `,
  activityMeta: css`
    display: flex;
    flex-wrap: wrap;
    gap: ${theme.spacing(0.5)};
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    overflow-wrap: anywhere;
  `,
  disclosure: css`
    margin-top: ${theme.spacing(2)};
    border-top: 1px solid ${theme.colors.border.weak};
    padding-top: ${theme.spacing(1)};
    summary {
      cursor: pointer;
      color: ${theme.colors.text.secondary};
      font-weight: ${theme.typography.fontWeightMedium};
    }
  `,
  labelGrid: css`
    display: grid;
    grid-template-columns: minmax(90px, 1fr) minmax(0, 2fr);
    gap: ${theme.spacing(0.5)} ${theme.spacing(1)};
    margin: ${theme.spacing(1)} 0 0;
    font-size: ${theme.typography.bodySmall.fontSize};
    dt {
      color: ${theme.colors.text.secondary};
      overflow-wrap: anywhere;
    }
    dd {
      margin: 0;
      overflow-wrap: anywhere;
    }
  `,
});
