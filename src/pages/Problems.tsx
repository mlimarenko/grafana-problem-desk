import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { css } from '@emotion/css';
import type { GrafanaTheme2, SelectableValue } from '@grafana/data';
import { config, locationService } from '@grafana/runtime';
import {
  Alert,
  Button,
  Field,
  Icon,
  Input,
  RadioButtonGroup,
  Select,
  Spinner,
  Tab,
  TabsBar,
  useStyles2,
} from '@grafana/ui';
import { DetailDrawer, type BusyAction } from '../components/DetailDrawer';
import { ProblemTable } from '../components/ProblemTable';
import { errorText, I18nProvider, useI18n, type Language } from '../i18n';
import { dateTimeLocalToISO, toDateTimeLocalValue } from '../i18n/format';
import {
  errorStatus,
  fetchDetail,
  fetchPolicy,
  fetchProblems,
  postAck,
  postAssign,
  postComment,
  postSync,
  postUnack,
  resolveProblem,
} from '../api';
import { toCSV, downloadCSV } from '../lib/csv';
import { resolveDeepLink, type DeepLinkError } from '../lib/deeplink';
import { correctedOffset, submitCommentDraft, validLabelFilter } from '../lib/workspace';
import {
  activeFilterCount,
  clearDeepLink,
  closeProblem,
  hasDeepLink,
  mergedQueryString,
  pageCount,
  parseQuery,
  queryString,
  rangeWindow,
  resetFilters,
  selectProblem,
  validateCustomRange,
  withView,
  type RangePreset,
  type SortKey,
  type View,
  type WorkspaceQuery,
} from '../lib/query';
import {
  classifyError,
  elapsedMs,
  problemTitle,
  levelDisplay,
  compareLevels,
  type Detail,
  type ErrorKind,
  type Listing,
  type Problem,
  type PolicyResponse,
} from '../model';
import { pluginId } from '../model';

export function Problems() {
  return (
    <I18nProvider>
      <Workspace />
    </I18nProvider>
  );
}

type ResolveState =
  | { status: 'idle' }
  | { status: 'resolving' }
  | { status: 'not-found' }
  | { status: 'ambiguous' }
  | { status: 'invalid'; error: DeepLinkError }
  | { status: 'error' };

function Workspace() {
  const styles = useStyles2(getStyles);
  const {
    language,
    setLanguage,
    timeZone,
    setTimeZone,
    zoneLabel,
    messages: m,
    formatDate,
    formatDuration,
    formatRelative,
  } = useI18n();
  const role = config.bootData.user.orgRole;
  const canWrite = role === 'Editor' || role === 'Admin';
  const canSync = role === 'Admin';

  const [query, setQueryState] = useState<WorkspaceQuery>(() => parseQuery(window.location.search));
  const [policy, setPolicy] = useState<PolicyResponse>();
  const [policyError, setPolicyError] = useState<ErrorKind>();
  const [policyAttempt, setPolicyAttempt] = useState(0);
  const queryRef = useRef(query);

  const adoptQuery = useCallback((next: WorkspaceQuery) => {
    queryRef.current = next;
    setQueryState(next);
  }, []);

  useEffect(() => {
    let active = true;
    setPolicyError(undefined);
    fetchPolicy().then((response) => {
      if (active) {
        adoptQuery(parseQuery(locationService.getLocation().search, response.doc.views));
        setPolicy(response);
      }
    }).catch((error) => { if (active) setPolicyError(classifyError(error)); });
    return () => { active = false; };
  }, [adoptQuery, policyAttempt]);

  const navigate = useCallback(
    (next: WorkspaceQuery, replace = false) => {
      adoptQuery(next);
      const location = locationService.getLocation();
      const search = mergedQueryString(location.search, next);
      const target = `${location.pathname}${search ? `?${search}` : ''}`;
      if (replace) {
        locationService.replace(target);
      } else {
        locationService.push(target);
      }
    },
    [adoptQuery]
  );

  useEffect(() => {
    const subscription = locationService.getLocationObservable().subscribe((location) => {
      const next = parseQuery(location.search, policy?.doc.views);
      if (queryString(next) !== queryString(queryRef.current)) {
        adoptQuery(next);
      }
    });
    return () => subscription.unsubscribe();
  }, [adoptQuery, policy]);

  useEffect(() => {
    const current = queryRef.current;
    if (current.problem && hasDeepLink(current)) {
      navigate(clearDeepLink(current), true);
    }
  }, [navigate]);

  const selectedId = query.problem;
  const [listing, setListing] = useState<Listing>();
  const [listLoading, setListLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [listError, setListError] = useState<ErrorKind>();
  const [lastLoadedAt, setLastLoadedAt] = useState(0);
  const [refreshToken, setRefreshToken] = useState(0);
  const lastListKey = useRef('');
  const listKey = useMemo(() => JSON.stringify(buildListSignature(query)), [query]);

  function buildListSignature(value: WorkspaceQuery): Record<string, string | number> {
    const copy = { ...value };
    return {
      view: copy.view,
      range: copy.range,
      from: copy.from,
      to: copy.to,
      state: copy.state,
      severity: copy.severity,
      workflow: copy.workflow,
      workState: copy.workState,
      host: copy.host,
      assignee: copy.assignee,
      assignment: copy.assignment,
      ack: copy.ack,
      search: copy.search,
      labelKey: copy.labelKey,
      labelValue: copy.labelValue,
      labelKeyPresent: Number(copy.labelKeyPresent),
      labelValuePresent: Number(copy.labelValuePresent),
      sort: copy.sort,
      order: copy.order,
      limit: copy.limit,
      offset: copy.offset,
    };
  }

  useEffect(() => {
    if (!policy) return;
    let active = true;
    const requestedQuery = queryRef.current;
    const isCurrent = () => active && JSON.stringify(buildListSignature(queryRef.current)) === listKey;
    const changed = lastListKey.current !== listKey;
    lastListKey.current = listKey;
    if (changed) {
      setListLoading(true);
      setRefreshing(false);
      setListing(undefined);
      setListError(undefined);
    } else {
      setRefreshing(true);
    }
    fetchProblems(requestedQuery, new Date())
      .then((data) => {
        if (!isCurrent()) {
          return;
        }
        const offset = correctedOffset(requestedQuery.offset, requestedQuery.limit, data.total);
        if (offset !== requestedQuery.offset) {
          active = false;
          setListing(undefined);
          setListLoading(true);
          setRefreshing(false);
          navigate({ ...queryRef.current, offset }, true);
          return;
        }
        setListing(data);
        setListError(undefined);
        setLastLoadedAt(Date.now());
      })
      .catch((error) => {
        if (isCurrent()) {
          setListError(classifyError(error));
        }
      })
      .finally(() => {
        if (isCurrent()) {
          setListLoading(false);
          setRefreshing(false);
        }
      });
    return () => {
      active = false;
    };
  }, [listKey, refreshToken, navigate, policy]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') {
        setRefreshToken((value) => value + 1);
      }
    }, 30000);
    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        setRefreshToken((value) => value + 1);
      }
    };
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, []);

  const [detail, setDetail] = useState<Detail>();
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<ErrorKind>();
  const [detailAttempt, setDetailAttempt] = useState(0);
  const detailSequence = useRef(0);

  useEffect(() => {
    if (!selectedId) {
      detailSequence.current += 1;
      setDetail(undefined);
      setDetailError(undefined);
      setDetailLoading(false);
      return;
    }
    const sequence = detailSequence.current + 1;
    detailSequence.current = sequence;
    setDetail((previous) => (previous?.problem.id === selectedId ? previous : undefined));
    setDetailLoading(true);
    setDetailError(undefined);
    fetchDetail(selectedId)
      .then((data) => {
        if (sequence !== detailSequence.current || queryRef.current.problem !== selectedId) {
          return;
        }
        setDetail(data);
        setDetailError(undefined);
      })
      .catch((error) => {
        if (sequence === detailSequence.current && queryRef.current.problem === selectedId) {
          setDetailError(classifyError(error));
        }
      })
      .finally(() => {
        if (sequence === detailSequence.current) {
          setDetailLoading(false);
        }
      });
  }, [selectedId, refreshToken, detailAttempt]);

  useEffect(() => {
    setActionError(undefined);
  }, [selectedId]);

  const [busyAction, setBusyAction] = useState<BusyAction>();
  const [actionError, setActionError] = useState<ErrorKind>();
  const [syncing, setSyncing] = useState(false);
  const [syncError, setSyncError] = useState<ErrorKind>();
  const [drafts, setDrafts] = useState<Record<string, string>>(() => Object.create(null));
  const [ownerDrafts, setOwnerDrafts] = useState<Record<string, string>>(() => Object.create(null));
  const draft = selectedId ? drafts[selectedId] ?? '' : '';
  const ownerDraft = selectedId ? ownerDrafts[selectedId] ?? detail?.problem.assignee ?? '' : '';

  const [searchDraft, setSearchDraft] = useState(query.search);
  const [hostDraft, setHostDraft] = useState(query.host);
  const [assigneeDraft, setAssigneeDraft] = useState(query.assignee);
  const [labelKeyDraft, setLabelKeyDraft] = useState(query.labelKey);
  const [labelValueDraft, setLabelValueDraft] = useState(query.labelValue);
  const [customOpen, setCustomOpen] = useState(query.range === 'custom');
  const [rangeFromDraft, setRangeFromDraft] = useState(toDateTimeLocalValue(query.from));
  const [rangeToDraft, setRangeToDraft] = useState(toDateTimeLocalValue(query.to));
  const [filterError, setFilterError] = useState('');
  const [rangeError, setRangeError] = useState('');

  useEffect(() => {
    setSearchDraft(query.search);
    setHostDraft(query.host);
    setAssigneeDraft(query.assignee);
    setLabelKeyDraft(query.labelKey);
    setLabelValueDraft(query.labelValue);
  }, [query.search, query.host, query.assignee, query.labelKey, query.labelValue]);

  useEffect(() => {
    setCustomOpen(query.range === 'custom');
    setRangeFromDraft(toDateTimeLocalValue(query.from));
    setRangeToDraft(toDateTimeLocalValue(query.to));
    setRangeError('');
  }, [query.range, query.from, query.to, query.view]);

  const [resolveState, setResolveState] = useState<ResolveState>({ status: 'idle' });
  const [resolveAttempt, setResolveAttempt] = useState(0);
  const deepLinkKey = JSON.stringify([
    query.fingerprint,
    query.startsAt,
    query.matchers,
    query.ruleUID,
    query.instance,
    query.device,
    query.mountpoint,
    query.name,
    query.datname,
  ]);

  useEffect(() => {
    if (queryRef.current.problem) {
      setResolveState({ status: 'idle' });
      return;
    }
    const resolution = resolveDeepLink(queryRef.current);
    if (!resolution) {
      setResolveState({ status: 'idle' });
      return;
    }
    if ('error' in resolution) {
      setResolveState({ status: 'invalid', error: resolution.error });
      return;
    }
    let active = true;
    setResolveState({ status: 'resolving' });
    resolveProblem(resolution.request)
      .then((problemId) => {
        if (!active) {
          return;
        }
        const current = queryRef.current;
        const currentKey = JSON.stringify([
          current.fingerprint,
          current.startsAt,
          current.matchers,
          current.ruleUID,
          current.instance,
          current.device,
          current.mountpoint,
          current.name,
          current.datname,
        ]);
        if (current.problem || currentKey !== deepLinkKey) {
          return;
        }
        setResolveState({ status: 'idle' });
        navigate(selectProblem(current, problemId), true);
      })
      .catch((error) => {
        if (!active) {
          return;
        }
        const status = errorStatus(error);
        if (status === 404) {
          setResolveState({ status: 'not-found' });
        } else if (status === 409) {
          setResolveState({ status: 'ambiguous' });
        } else if (status === 400) {
          setResolveState({ status: 'invalid', error: query.fingerprint ? 'fingerprint' : 'matchers' });
        } else {
          setResolveState({ status: 'error' });
        }
      });
    return () => {
      active = false;
    };
  }, [deepLinkKey, query.problem, resolveAttempt, navigate]);

  function severityLabel(problem: Problem): string {
    return levelDisplay(problem, listing?.levels ?? [], listing?.severityKey ?? '', language).label;
  }

  function openProblem(problem: Problem) {
    if (problem.id === selectedId) {
      return;
    }
    if (!confirmDiscard()) {
      return;
    }
    setActionError(undefined);
    navigate(selectProblem(queryRef.current, problem.id));
  }

  function confirmDiscard(): boolean {
    if (!selectedId) {
      return true;
    }
    if (!drafts[selectedId]?.trim()) {
      return true;
    }
    if (!window.confirm(m.drawer.discardConfirm)) {
      return false;
    }
    discardDraft(selectedId);
    return true;
  }

  function discardDraft(problemId: string) {
    setDrafts((previous) => {
      if (!(problemId in previous)) {
        return previous;
      }
      const next = Object.assign(Object.create(null), previous);
      delete next[problemId];
      return next;
    });
  }

  function closeDrawer() {
    if (!confirmDiscard()) {
      return;
    }
    setActionError(undefined);
    navigate(closeProblem(queryRef.current), true);
  }

  function setDraft(value: string) {
    if (!selectedId) {
      return;
    }
    setDrafts((previous) => Object.assign(Object.create(null), previous, { [selectedId]: value }));
  }

  function setOwnerDraft(value: string) {
    if (!selectedId) {
      return;
    }
    setOwnerDrafts((previous) => Object.assign(Object.create(null), previous, { [selectedId]: value }));
  }

  function updateDetailProblem(updated: Problem) {
    if (!updated || typeof updated.id !== 'string') {
      return;
    }
    if (queryRef.current.problem === updated.id) {
      detailSequence.current += 1;
    }
    setDetail((previous) => (previous && previous.problem.id === updated.id ? { ...previous, problem: updated } : previous));
  }

  async function runAction(action: BusyAction, problemId: string, operation: () => Promise<void>) {
    setBusyAction(action);
    setActionError(undefined);
    try {
      await operation();
      setRefreshToken((value) => value + 1);
    } catch (error) {
      if (queryRef.current.problem === problemId) {
        setActionError(classifyError(error));
      }
    } finally {
      setBusyAction(undefined);
    }
  }

  function acknowledge() {
    const problemId = queryRef.current.problem;
    if (!problemId) {
      return;
    }
    void runAction('ack', problemId, async () => {
      updateDetailProblem(await postAck(problemId));
    });
  }

  function unacknowledge() {
    const problemId = queryRef.current.problem;
    if (!problemId) {
      return;
    }
    void runAction('unack', problemId, async () => {
      updateDetailProblem(await postUnack(problemId));
    });
  }

  function assignOwner() {
    const problemId = queryRef.current.problem;
    if (!problemId) {
      return;
    }
    const value = ownerDraft.trim();
    void runAction('assign', problemId, async () => {
      updateDetailProblem(await postAssign(problemId, value));
      setOwnerDrafts((previous) => {
        if (previous[problemId] !== undefined && previous[problemId] !== value) {
          return previous;
        }
        const next = Object.assign(Object.create(null), previous);
        delete next[problemId];
        return next;
      });
    });
  }

  function addComment() {
    const problemId = selectedId;
    const submittedDraft = draft;
    const body = submittedDraft.trim();
    if (!problemId || queryRef.current.problem !== problemId || !body) {
      return;
    }
    void runAction('comment', problemId, async () => {
      await submitCommentDraft(problemId, submittedDraft, postComment, setDrafts);
    });
  }

  async function sync() {
    setSyncing(true);
    setSyncError(undefined);
    try {
      await postSync();
      setRefreshToken((value) => value + 1);
    } catch (error) {
      setSyncError(classifyError(error));
    } finally {
      setSyncing(false);
    }
  }

  function applyFilters(event: React.FormEvent) {
    event.preventDefault();
    const key = labelKeyDraft;
    const value = labelValueDraft;
    if (!validLabelFilter(key, value)) {
      setFilterError(!key && value ? m.filters.labelPairError : language === 'ru'
        ? 'Метка не должна превышать 256 байт, значение — 1024 байта.'
        : 'Label must be at most 256 bytes and value at most 1024 bytes.');
      return;
    }
    setFilterError('');
    navigate({
      ...queryRef.current,
      search: searchDraft.trim(),
      host: hostDraft.trim(),
      assignee: assigneeDraft.trim(),
      labelKey: key,
      labelValue: value,
      labelKeyPresent: key !== '',
      labelValuePresent: key !== '',
      offset: 0,
    });
  }

  function resetAllFilters() {
    setFilterError('');
    setSearchDraft('');
    setHostDraft('');
    setAssigneeDraft('');
    setLabelKeyDraft('');
    setLabelValueDraft('');
    navigate(resetFilters(queryRef.current));
  }

  function chooseRange(value: RangePreset) {
    if (value === 'custom') {
      const currentWindow = rangeWindow(queryRef.current, new Date());
      setCustomOpen(true);
      setRangeError('');
      if (currentWindow) {
        navigate({
          ...queryRef.current,
          range: 'custom',
          rangeExplicit: true,
          from: currentWindow.from.toISOString(),
          to: currentWindow.to.toISOString(),
          offset: 0,
        });
      }
      return;
    }
    setCustomOpen(false);
    setRangeError('');
    navigate({ ...queryRef.current, range: value, rangeExplicit: true, from: '', to: '', offset: 0 });
  }

  function applyCustomRange() {
    const from = dateTimeLocalToISO(rangeFromDraft);
    const to = dateTimeLocalToISO(rangeToDraft);
    if (!from || !to) {
      setRangeError(m.range.invalidDate);
      return;
    }
    const validation = validateCustomRange(from, to, queryRef.current.view);
    if (validation === 'invalid') {
      setRangeError(m.range.invalidDate);
      return;
    }
    if (validation === 'order') {
      setRangeError(m.range.invalidOrder);
      return;
    }
    if (validation === 'too-long') {
      setRangeError(m.range.tooLong);
      return;
    }
    setRangeError('');
    navigate({ ...queryRef.current, range: 'custom', rangeExplicit: true, from, to, offset: 0 });
  }

  function applySingleFilter(patch: Partial<WorkspaceQuery>) {
    setFilterError('');
    navigate({ ...queryRef.current, ...patch, offset: 0 });
  }

  function changeView(view: View) {
    setFilterError('');
    navigate(withView(queryRef.current, view, policy?.doc.views));
  }

  function exportPage() {
    if (!listing?.problems.length) {
      return;
    }
    const headers = [
      m.table.severity,
      m.table.problem,
      m.table.status,
      m.table.started,
      m.table.duration,
      m.table.owner,
      m.table.ack,
    ];
    const rows = listing.problems.map((problem) => [
      severityLabel(problem),
      problemTitle(problem) || m.table.untitled,
      problem.status === 'active' ? m.status.active : m.status.recovered,
      formatDate(problem.startsAt),
      (() => {
        const duration = elapsedMs(problem, Date.now());
        return duration === undefined ? '—' : formatDuration(duration);
      })(),
      problem.assignee || m.filters.unassigned,
      problem.acknowledgedBy ? m.ack.acknowledgedBy(problem.acknowledgedBy) : m.ack.notAcknowledged,
    ]);
    downloadCSV(`problem-desk-${new Date().toISOString().slice(0, 10)}.csv`, toCSV(headers, rows));
  }

  const summary = listing?.summary ?? {
    active: 0,
    recovered: 0,
    acknowledged: 0,
    unacknowledged: 0,
    unassigned: 0,
    byLevel: Object.create(null) as Record<string, number>,
  };
  const levelStats = (listing?.levels ?? []).slice().sort(compareLevels).map((level) => ({
    label: level.name[language], value: summary.byLevel[level.id] ?? 0, tone: '',
  }));
  const knownLevelIds = new Set((listing?.levels ?? []).map((level) => level.id));
  for (const [id, value] of Object.entries(summary.byLevel)) {
    if (!knownLevelIds.has(id)) levelStats.push({ label: m.summary.retired(id), value, tone: '' });
  }
  const total = listing?.total ?? 0;
  const now = lastLoadedAt || Date.now();
  const windowRange = rangeWindow(query, new Date(lastLoadedAt || Date.now()));
  const windowLabel = windowRange
    ? m.range.window(formatDate(windowRange.from.toISOString()), formatDate(windowRange.to.toISOString()))
    : '';  const activeFilters = activeFilterCount(query);
  const rangeOptions: Array<SelectableValue<RangePreset>> = [
    { label: m.range.oneHour, value: '1h' },
    { label: m.range.twentyFourHours, value: '24h' },
    { label: m.range.sevenDays, value: '7d' },
    { label: m.range.thirtyDays, value: '30d' },
    { label: m.range.custom, value: 'custom' },
  ];
  const severityOptions: Array<SelectableValue<string>> = [
    { label: m.filters.severityAll, value: '' },
    ...(listing?.levels ?? []).slice().sort(compareLevels).map((level) => ({ label: level.name[language], value: level.id })),
  ];
  if (query.severity && !severityOptions.some((option) => option.value === query.severity)) {
    severityOptions.push({ label: m.summary.retired(query.severity), value: query.severity });
  }
  const workflowOptions: Array<SelectableValue<string>> = [
    { label: m.filters.workflowAll, value: '' },
    ...(policy?.doc.workflows ?? []).map((workflow) => ({ label: workflow.name[language], value: workflow.id })),
  ];
  if (query.workflow && !workflowOptions.some((option) => option.value === query.workflow)) workflowOptions.push({ label: m.summary.retired(query.workflow), value: query.workflow });
  const states = new Map<string, string>();
  for (const workflow of policy?.doc.workflows ?? []) {
    if (!query.workflow || query.workflow === workflow.id) {
      for (const state of workflow.states) if (!states.has(state.id)) states.set(state.id, state.name[language]);
    }
  }
  const workStateOptions: Array<SelectableValue<string>> = [{ label: m.filters.workStateAll, value: '' }, ...Array.from(states, ([value, label]) => ({ value, label }))];
  if (query.workState && !states.has(query.workState)) workStateOptions.push({ label: m.summary.retired(query.workState), value: query.workState });
  const assignmentOptions: Array<SelectableValue<WorkspaceQuery['assignment']>> = [
    { label: m.filters.assignmentAll, value: 'all' },
    { label: m.filters.assigned, value: 'assigned' },
    { label: m.filters.unassigned, value: 'unassigned' },
  ];
  const ackOptions: Array<SelectableValue<WorkspaceQuery['ack']>> = [
    { label: m.filters.ackAll, value: 'all' },
    { label: m.filters.acknowledged, value: 'acknowledged' },
    { label: m.filters.unacknowledged, value: 'unacknowledged' },
  ];
  const stateOptions: Array<SelectableValue<WorkspaceQuery['state']>> = [
    { label: m.filters.stateAll, value: 'all' },
    { label: m.filters.stateActive, value: 'active' },
    { label: m.filters.stateRecovered, value: 'recovered' },
  ];
  const sortOptions: Array<SelectableValue<SortKey>> = [
    { label: m.filters.sortSeverity, value: 'severity' },
    { label: m.filters.sortStarted, value: 'started' },
    { label: m.filters.sortDuration, value: 'duration' },
    { label: m.filters.sortRecovered, value: 'recovered' },
  ];
  const orderOptions: Array<SelectableValue<WorkspaceQuery['order']>> = [
    { label: m.filters.orderDesc, value: 'desc' },
    { label: m.filters.orderAsc, value: 'asc' },
  ];
  const pageSizeOptions: Array<SelectableValue<WorkspaceQuery['limit']>> = [
    { label: '25', value: 25 },
    { label: '50', value: 50 },
    { label: '100', value: 100 },
  ];
  const hostOptions = (listing?.facets.hosts ?? []).map((value) => ({ label: value, value }));
  const assigneeOptions = (listing?.facets.assignees ?? []).map((value) => ({ label: value, value }));
  const labelOptions = (listing?.facets.labelKeys ?? []).map((value) => ({ label: value, value }));
  if (query.labelKey && !labelOptions.some((option) => option.value === query.labelKey)) {
    labelOptions.unshift({ label: query.labelKey, value: query.labelKey });
  }
  const empty = listing && listing.total === 0 && !listLoading;

  return (
    <main className={styles.page}>
      <header className={styles.header}>
        <div className={styles.heading}>
          <div className={styles.titleRow}>
            <Icon name="bell" size="lg" />
            <h1 className={styles.h1}>{m.app.title}</h1>
          </div>
          <p className={styles.subtitle}>{m.app.subtitle}</p>
        </div>
        <div className={styles.headerActions}>
          <Select
            aria-label={m.app.language}
            width={13}
            value={language}
            options={[
              { label: 'English', value: 'en' },
              { label: 'Русский', value: 'ru' },
            ]}
            onChange={(option) => option.value && setLanguage(option.value as Language)}
          />
          <Button
            variant="secondary"
            size="sm"
            icon="clock-nine"
            title={m.app.timeZone}
            aria-label={`${m.app.timeZone}: ${zoneLabel}`}
            onClick={() => setTimeZone(timeZone === 'UTC' ? 'local' : 'UTC')}
          >
            {zoneLabel}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            icon="sync"
            disabled={refreshing}
            onClick={() => setRefreshToken((value) => value + 1)}
          >
            {refreshing ? m.common.refreshing : m.app.refresh}
          </Button>
          {canSync && (
            <Button variant="secondary" size="sm" icon="cloud-upload" disabled={syncing} onClick={() => void sync()}>
              {syncing ? m.app.syncing : m.app.syncSource}
            </Button>
          )}
          <a className={styles.configLink} href={`${config.appSubUrl}/plugins/${pluginId}?page=configuration`}>
            <Icon name="cog" size="sm" />
            {m.app.configuration}
          </a>
        </div>
      </header>

      <TabsBar className={styles.tabs} aria-label={m.tabs.label}>
        <Tab
          label={m.tabs.problems}
          icon="fire"
          active={query.view === 'problems'}
          tooltip={m.tabs.problemsHint}
          counter={query.view === 'problems' ? (listing ? total : null) : null}
          onChangeTab={() => changeView('problems')}
        />
        <Tab
          label={m.tabs.recent}
          icon="clock-nine"
          active={query.view === 'recent'}
          tooltip={m.tabs.recentHint}
          onChangeTab={() => changeView('recent')}
        />
        <Tab
          label={m.tabs.history}
          icon="history"
          active={query.view === 'history'}
          tooltip={m.tabs.historyHint}
          onChangeTab={() => changeView('history')}
        />
      </TabsBar>

      <section className={styles.stats} aria-label={m.filters.label}>
        {[
          { label: m.summary.matchingLabel, value: listing ? total : '—', tone: '' },
          { label: m.summary.active, value: listing ? summary.active : '—', tone: 'critical' },
          { label: m.summary.recovered, value: listing ? summary.recovered : '—', tone: 'success' },
          ...levelStats,
          { label: m.summary.acknowledged, value: listing ? summary.acknowledged : '—', tone: 'success' },
          { label: m.summary.unacknowledged, value: listing ? summary.unacknowledged : '—', tone: '' },
          { label: m.summary.unassigned, value: listing ? summary.unassigned : '—', tone: '' },
        ].map((stat, index) => (
          <div className={styles.stat} key={`${index}:${stat.label}`}>
            <span className={styles.statLabel}>{stat.label}</span>
            <strong className={stat.tone ? styles.statValue(stat.tone) : styles.statValuePlain}>{stat.value}</strong>
          </div>
        ))}
      </section>

      <DeepLinkNotice
        state={resolveState}
        startsAt={query.startsAt}
        onRetry={() => setResolveAttempt((value) => value + 1)}
      />

      {syncError && (
        <Alert title={m.errors.syncTitle} severity="error">
          {errorText(syncError, m)}
        </Alert>
      )}
      {policyError && (
        <Alert title={m.errors.listTitle} severity="error">
          {errorText(policyError, m)} <Button size="sm" variant="secondary" onClick={() => setPolicyAttempt((value) => value + 1)}>{m.common.retry}</Button>
        </Alert>
      )}
      {listError && (
        <Alert title={m.errors.listTitle} severity="error">
          <p>{errorText(listError, m)}</p>
          <Button size="sm" variant="secondary" onClick={() => setRefreshToken((value) => value + 1)}>
            {m.common.retry}
          </Button>
        </Alert>
      )}
      {listing?.source.error && (
        <Alert title={m.source.staleTitle} severity="warning">
          {m.source.staleBody}
        </Alert>
      )}

      <section className={styles.filtersPanel} aria-label={m.filters.label}>
        <div className={styles.filterHeading}>
          <span className={styles.filterTitle}>
            <Icon name="filter" size="sm" />
            {m.filters.label}
            {activeFilters > 0 && <span className={styles.filterCount}>{activeFilters}</span>}
          </span>
          <span className={styles.rangeHint} title={m.range.rolling}>
            {windowLabel && (
              <>
                <Icon name="calendar-alt" size="sm" /> {windowLabel}
              </>
            )}
          </span>
        </div>

        {query.view !== 'problems' && (
          <div className={styles.rangeRow}>
            <Field label={m.range.label} className={styles.rangeField}>
              <RadioButtonGroup<RangePreset>
                options={rangeOptions}
                value={query.range}
                onChange={(value) => value && chooseRange(value)}
                aria-label={m.range.label}
              />
            </Field>
            {customOpen && (
              <div className={styles.customRange}>
                <Field label={m.range.from}>
                  <Input
                    type="datetime-local"
                    aria-label={m.range.from}
                    value={rangeFromDraft}
                    onChange={(event) => setRangeFromDraft(event.currentTarget.value)}
                  />
                </Field>
                <Field label={m.range.to}>
                  <Input
                    type="datetime-local"
                    aria-label={m.range.to}
                    value={rangeToDraft}
                    onChange={(event) => setRangeToDraft(event.currentTarget.value)}
                  />
                </Field>
                <Button size="sm" variant="secondary" icon="calendar-alt" onClick={applyCustomRange}>
                  {m.range.apply}
                </Button>
              </div>
            )}
            {rangeError && <span className={styles.fieldError}>{rangeError}</span>}
          </div>
        )}

        <form onSubmit={applyFilters}>
          <div className={styles.filterRow}>
            <Field label={m.filters.search} className={styles.searchField}>
              <Input
                type="search"
                aria-label={m.filters.search}
                placeholder={m.filters.searchPlaceholder}
                value={searchDraft}
                onChange={(event) => setSearchDraft(event.currentTarget.value)}
              />
            </Field>
            <Field label={m.filters.severity} className={styles.field}>
              <Select
                aria-label={m.filters.severity}
                options={severityOptions}
                value={query.severity}
                onChange={(option) => applySingleFilter({ severity: option.value ?? '' })}
              />
            </Field>
            <Field label={m.filters.workflow} className={styles.field}>
              <Select aria-label={m.filters.workflow} options={workflowOptions} value={query.workflow} onChange={(option) => applySingleFilter({ workflow: option.value ?? '' })} />
            </Field>
            <Field label={m.filters.workState} className={styles.field}>
              <Select aria-label={m.filters.workState} options={workStateOptions} value={query.workState} onChange={(option) => applySingleFilter({ workState: option.value ?? '' })} />
            </Field>
            <Field label={m.filters.host} className={styles.field}>
              <Input
                aria-label={m.filters.host}
                list="problem-desk-host-list"
                placeholder={m.filters.hostPlaceholder}
                value={hostDraft}
                onChange={(event) => setHostDraft(event.currentTarget.value)}
              />
            </Field>
            <Field label={m.filters.assignee} className={styles.field}>
              <Input
                aria-label={m.filters.assignee}
                list="problem-desk-assignee-list"
                placeholder={m.filters.assigneePlaceholder}
                value={assigneeDraft}
                onChange={(event) => setAssigneeDraft(event.currentTarget.value)}
              />
            </Field>
            <Field label={m.filters.assignment} className={styles.field}>
              <Select
                aria-label={m.filters.assignment}
                options={assignmentOptions}
                value={query.assignment}
                onChange={(option) => applySingleFilter({ assignment: option.value ?? 'all' })}
              />
            </Field>
            <Field label={m.filters.ack} className={styles.field}>
              <Select
                aria-label={m.filters.ack}
                options={ackOptions}
                value={query.ack}
                onChange={(option) => applySingleFilter({ ack: option.value ?? 'all' })}
              />
            </Field>
            {query.view !== 'problems' && (
              <Field label={m.filters.state} className={styles.field}>
                <Select
                  aria-label={m.filters.state}
                  options={stateOptions}
                  value={query.state}
                  onChange={(option) => applySingleFilter({ state: option.value ?? 'all' })}
                />
              </Field>
            )}
          </div>
          <div className={styles.filterRow}>
            <Field label={m.filters.labelKey} className={styles.field}>
              <Input
                aria-label={m.filters.labelKey}
                placeholder={m.filters.labelKeyPlaceholder}
                list="problem-desk-label-list"
                value={labelKeyDraft}
                onChange={(event) => setLabelKeyDraft(event.currentTarget.value)}
              />
            </Field>
            <Field label={m.filters.labelValue} className={styles.field}>
              <Input
                aria-label={m.filters.labelValue}
                placeholder={m.filters.labelValuePlaceholder}
                value={labelValueDraft}
                onChange={(event) => setLabelValueDraft(event.currentTarget.value)}
              />
            </Field>
            <Field label={m.filters.sort} className={styles.field}>
              <Select
                aria-label={m.filters.sort}
                options={sortOptions}
                value={query.sort}
                onChange={(option) => applySingleFilter({ sort: option.value ?? 'severity' })}
              />
            </Field>
            <Field label={m.filters.order} className={styles.field}>
              <Select
                aria-label={m.filters.order}
                options={orderOptions}
                value={query.order}
                onChange={(option) => applySingleFilter({ order: option.value ?? 'desc' })}
              />
            </Field>
            <div className={styles.filterActions}>
              <Button type="submit" size="sm">
                {m.filters.apply}
              </Button>
              <Button type="button" size="sm" variant="secondary" onClick={resetAllFilters} disabled={activeFilters === 0}>
                {m.filters.reset}
              </Button>
            </div>
          </div>
          {filterError && <div className={styles.fieldError}>{filterError}</div>}
          {(hostOptions.length > 0 || assigneeOptions.length > 0 || labelOptions.length > 0) && (
            <>
              <datalist id="problem-desk-host-list">
                {hostOptions.map((option) => (
                  <option value={option.value} key={option.value} />
                ))}
              </datalist>
              <datalist id="problem-desk-assignee-list">
                {assigneeOptions.map((option) => (
                  <option value={option.value} key={option.value} />
                ))}
              </datalist>
              <datalist id="problem-desk-label-list">
                {labelOptions.map((option) => (
                  <option value={option.value} key={option.value} />
                ))}
              </datalist>
            </>
          )}
        </form>
      </section>

      <div className={styles.meta} role="status" aria-live="polite">
        <span>
          {listing ? m.summary.matching(total) : m.common.loading}
          {refreshing && <span className={styles.refreshing}> · {m.common.refreshing}</span>}
        </span>
        <span>
          {listing
            ? m.source.lastSuccess(
                listing.source.lastSuccess ? formatDate(listing.source.lastSuccess) : m.source.never
              )
            : '—'}
        </span>
        {lastLoadedAt > 0 && <span>{m.source.updated(formatRelative(new Date(lastLoadedAt).toISOString()))}</span>}
        <span title={m.app.timeZone}>{zoneLabel}</span>
      </div>

      <section className={styles.tableCard} aria-busy={listLoading || refreshing}>
        <div className={styles.tableToolbar}>
          <span className={styles.tableToolbarTitle}>
            <Icon name="list-ul" size="sm" />
            {m.table.caption}
          </span>
          <Button
            variant="secondary"
            size="sm"
            icon="download-alt"
            disabled={!listing?.problems.length}
            onClick={exportPage}
          >
            {m.csv.exportPage}
          </Button>
        </div>
        {listLoading && !listing && <TableSkeleton />}
        {empty && (
          <div className={styles.empty}>
            <Icon name="check-circle" size="xl" />
            <h2>{emptyTitle(query.view, m)}</h2>
            <p>{emptyBody(query.view, m)}</p>
            {activeFilters > 0 && (
              <Button size="sm" variant="secondary" onClick={resetAllFilters}>
                {m.empty.resetFilters}
              </Button>
            )}
          </div>
        )}
        {listing && listing.problems.length > 0 && (
          <ProblemTable problems={listing.problems} levels={listing.levels} severityKey={listing.severityKey} columns={policy?.doc.views.columns ?? []} selectedId={selectedId} now={now} onOpen={openProblem} />
        )}
        {listing && (
          <div className={styles.pagination}>
            <Button
              size="sm"
              variant="secondary"
              disabled={query.offset === 0}
              onClick={() => navigate({ ...queryRef.current, offset: Math.max(0, query.offset - query.limit) })}
            >
              {m.pagination.previous}
            </Button>
            <span aria-live="polite">
              {total === 0
                ? m.summary.matching(0)
                : m.pagination.range(query.offset + 1, Math.min(query.offset + query.limit, total), total)}
              {' · '}
              {m.pagination.page(Math.floor(query.offset / query.limit) + 1, pageCount(total, query.limit))}
            </span>
            <Button
              size="sm"
              variant="secondary"
              disabled={query.offset + query.limit >= total}
              onClick={() => navigate({ ...queryRef.current, offset: query.offset + query.limit })}
            >
              {m.pagination.next}
            </Button>
            <span className={styles.pageSize}>
              <span>{m.pagination.pageSize}</span>
              <Select<WorkspaceQuery['limit']>
                aria-label={m.pagination.pageSize}
                width={8}
                options={pageSizeOptions}
                value={query.limit}
                onChange={(option) =>
                  option.value && navigate({ ...queryRef.current, limit: option.value, offset: 0 })
                }
              />
            </span>
          </div>
        )}
      </section>

      <DetailDrawer
        selectedId={selectedId}
        detail={detail}
        loading={detailLoading}
        error={detailError}
        actionError={actionError}
        busyAction={busyAction}
        draft={draft}
        ownerDraft={ownerDraft}
        canWrite={canWrite}
        levels={listing?.levels ?? policy?.doc.levels ?? []}
        severityKey={listing?.severityKey ?? policy?.doc.severityKey ?? ''}
        policy={policy?.doc}
        onWorkflowChanged={() => setRefreshToken((value) => value + 1)}
        onDraftChange={setDraft}
        onOwnerDraftChange={setOwnerDraft}
        onClose={closeDrawer}
        onRetry={() => setDetailAttempt((value) => value + 1)}
        onAck={acknowledge}
        onUnack={unacknowledge}
        onAssign={assignOwner}
        onComment={addComment}
      />
    </main>
  );
}

function emptyTitle(view: View, messages: ReturnType<typeof useI18n>['messages']): string {
  if (view === 'recent') return messages.empty.recentTitle;
  if (view === 'history') return messages.empty.historyTitle;
  return messages.empty.problemsTitle;
}

function emptyBody(view: View, messages: ReturnType<typeof useI18n>['messages']): string {
  if (view === 'recent') return messages.empty.recentBody;
  if (view === 'history') return messages.empty.historyBody;
  return messages.empty.problemsBody;
}

function TableSkeleton() {
  const styles = useStyles2(getStyles);
  return (
    <div className={styles.skeleton} aria-hidden="true">
      {Array.from({ length: 8 }).map((_, index) => (
        <div className={styles.skeletonRow} key={index} />
      ))}
    </div>
  );
}

function DeepLinkNotice({
  state,
  startsAt,
  onRetry,
}: {
  state: ResolveState;
  startsAt: string;
  onRetry: () => void;
}) {
  const styles = useStyles2(getStyles);
  const { messages: m, formatDate } = useI18n();
  if (state.status === 'idle') {
    return null;
  }
  if (state.status === 'resolving') {
    return (
      <div className={styles.linkNotice} role="status">
        <Spinner size={16} />
        <span>{m.deepLink.resolving}</span>
      </div>
    );
  }
  if (state.status === 'not-found') {
    return (
      <Alert title={m.deepLink.notFoundTitle} severity="warning">
        <p>{startsAt ? m.deepLink.notFoundBodyStart(formatDate(startsAt)) : m.deepLink.notFoundBody}</p>
        <Button size="sm" variant="secondary" onClick={onRetry}>
          {m.deepLink.retry}
        </Button>
      </Alert>
    );
  }
  if (state.status === 'ambiguous') {
    return (
      <Alert title={m.deepLink.ambiguousTitle} severity="error">
        {m.deepLink.ambiguousBody}
      </Alert>
    );
  }
  if (state.status === 'invalid') {
    return (
      <Alert title={m.deepLink.invalidTitle} severity="error">
        {state.error === 'fingerprint' ? m.deepLink.invalidFingerprintBody : m.deepLink.invalidMatchersBody}
      </Alert>
    );
  }
  return (
    <Alert title={m.deepLink.errorTitle} severity="error">
      <p>{m.deepLink.errorBody}</p>
      <Button size="sm" variant="secondary" onClick={onRetry}>
        {m.deepLink.retry}
      </Button>
    </Alert>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  page: css`
    box-sizing: border-box;
    width: 100%;
    max-width: 1920px;
    min-width: 0;
    margin: 0 auto;
    padding: ${theme.spacing(2.5)};
    @media (max-width: 640px) {
      padding: ${theme.spacing(1.5)};
    }
  `,
  header: css`
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: ${theme.spacing(2)};
    flex-wrap: wrap;
  `,
  heading: css`
    min-width: 240px;
    flex: 1 1 360px;
  `,
  titleRow: css`
    display: flex;
    align-items: center;
    gap: ${theme.spacing(1)};
    color: ${theme.colors.primary.text};
  `,
  h1: css`
    margin: 0;
    font-size: ${theme.typography.h3.fontSize};
    line-height: ${theme.typography.h3.lineHeight};
  `,
  subtitle: css`
    margin: ${theme.spacing(0.5)} 0 0;
    color: ${theme.colors.text.secondary};
    max-width: 78ch;
  `,
  headerActions: css`
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: ${theme.spacing(1)};
    flex-wrap: wrap;
    flex: 0 1 auto;
  `,
  configLink: css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    color: ${theme.colors.text.link};
    white-space: nowrap;
  `,
  tabs: css`
    margin-top: ${theme.spacing(1.5)};
    overflow-x: auto;
  `,
  stats: css`
    display: flex;
    flex-wrap: wrap;
    align-items: stretch;
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.secondary};
    margin: ${theme.spacing(1.5)} 0;
    padding: ${theme.spacing(0.5)} ${theme.spacing(1)};
  `,
  stat: css`
    display: flex;
    align-items: baseline;
    gap: ${theme.spacing(0.5)};
    padding: ${theme.spacing(0.5)} ${theme.spacing(1.25)};
    border-right: 1px solid ${theme.colors.border.weak};
    &:last-child {
      border-right: 0;
    }
    @media (max-width: 640px) {
      padding: ${theme.spacing(0.5)} ${theme.spacing(0.75)};
    }
  `,
  statLabel: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    white-space: nowrap;
  `,
  statValue: (tone: string) => css`
    font-variant-numeric: tabular-nums;
    color: ${tone === 'critical'
      ? theme.colors.error.text
      : tone === 'warning'
        ? theme.colors.warning.text
        : theme.colors.success.text};
  `,
  statValuePlain: css`
    font-variant-numeric: tabular-nums;
    color: ${theme.colors.text.primary};
  `,
  linkNotice: css`
    display: flex;
    align-items: center;
    gap: ${theme.spacing(1)};
    padding: ${theme.spacing(1)} ${theme.spacing(1.5)};
    margin-bottom: ${theme.spacing(1.5)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.secondary};
    color: ${theme.colors.text.secondary};
  `,
  filtersPanel: css`
    padding: ${theme.spacing(1.5)};
    margin-bottom: ${theme.spacing(1)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.canvas};
  `,
  filterHeading: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: ${theme.spacing(1)};
    flex-wrap: wrap;
    margin-bottom: ${theme.spacing(1)};
  `,
  filterTitle: css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.75)};
    color: ${theme.colors.text.secondary};
    font-weight: ${theme.typography.fontWeightMedium};
  `,
  filterCount: css`
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 20px;
    height: 20px;
    padding: 0 6px;
    border-radius: 999px;
    background: ${theme.colors.primary.main};
    color: ${theme.colors.primary.contrastText};
    font-size: 12px;
  `,
  rangeHint: css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
  rangeRow: css`
    display: flex;
    align-items: flex-end;
    flex-wrap: wrap;
    gap: ${theme.spacing(1.5)};
    margin-bottom: ${theme.spacing(1)};
  `,
  rangeField: css`
    flex: 1 1 420px;
    min-width: 0;
    margin-bottom: 0;
  `,
  customRange: css`
    display: flex;
    align-items: flex-end;
    flex-wrap: wrap;
    gap: ${theme.spacing(1)};
    > div {
      min-width: 160px;
      margin-bottom: 0;
    }
  `,
  filterRow: css`
    display: flex;
    align-items: flex-end;
    flex-wrap: wrap;
    gap: ${theme.spacing(1)};
    margin-bottom: ${theme.spacing(1)};
  `,
  field: css`
    flex: 0 1 168px;
    min-width: 140px;
    margin-bottom: 0;
    @media (max-width: 640px) {
      flex: 1 1 100%;
    }
  `,
  searchField: css`
    flex: 1 1 260px;
    min-width: 200px;
    margin-bottom: 0;
    @media (max-width: 640px) {
      flex: 1 1 100%;
    }
  `,
  filterActions: css`
    display: flex;
    align-items: center;
    gap: ${theme.spacing(1)};
    margin-bottom: 2px;
    flex-wrap: wrap;
  `,
  fieldError: css`
    color: ${theme.colors.error.text};
    font-size: ${theme.typography.bodySmall.fontSize};
    margin: ${theme.spacing(0.5)} 0;
  `,
  meta: css`
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: ${theme.spacing(1.5)};
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    margin: ${theme.spacing(1)} 0;
  `,
  refreshing: css`
    color: ${theme.colors.text.disabled};
  `,
  tableCard: css`
    min-width: 0;
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.canvas};
    overflow: hidden;
  `,
  tableToolbar: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: ${theme.spacing(1)};
    flex-wrap: wrap;
    padding: ${theme.spacing(1)} ${theme.spacing(1.5)};
    border-bottom: 1px solid ${theme.colors.border.weak};
  `,
  tableToolbarTitle: css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.75)};
    color: ${theme.colors.text.secondary};
    font-weight: ${theme.typography.fontWeightMedium};
  `,
  empty: css`
    padding: ${theme.spacing(6)} ${theme.spacing(3)};
    text-align: center;
    color: ${theme.colors.text.secondary};
    h2 {
      margin: ${theme.spacing(1.5)} 0 ${theme.spacing(0.5)};
      color: ${theme.colors.text.primary};
      font-size: ${theme.typography.h4.fontSize};
    }
    p {
      max-width: 62ch;
      margin: 0 auto ${theme.spacing(2)};
    }
  `,
  skeleton: css`
    padding: ${theme.spacing(1.5)};
  `,
  skeletonRow: css`
    height: 42px;
    margin-bottom: ${theme.spacing(0.75)};
    border-radius: ${theme.shape.radius.default};
    background: linear-gradient(
      90deg,
      ${theme.colors.background.secondary} 25%,
      ${theme.colors.action.hover} 50%,
      ${theme.colors.background.secondary} 75%
    );
    background-size: 200% 100%;
    animation: shimmer 1.4s ease-in-out infinite;
    @keyframes shimmer {
      from {
        background-position: 200% 0;
      }
      to {
        background-position: -200% 0;
      }
    }
    @media (prefers-reduced-motion: reduce) {
      animation: none;
    }
  `,
  pagination: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: ${theme.spacing(1)};
    flex-wrap: wrap;
    padding: ${theme.spacing(1)} ${theme.spacing(1.5)};
    border-top: 1px solid ${theme.colors.border.weak};
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
  pageSize: css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.75)};
    white-space: nowrap;
  `,
});
