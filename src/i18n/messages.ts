export type Language = 'en' | 'ru';

function enCount(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

function ruCount(count: number, one: string, few: string, many: string): string {
  const mod10 = count % 10;
  const mod100 = count % 100;
  if (mod10 === 1 && mod100 !== 11) {
    return `${count} ${one}`;
  }
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) {
    return `${count} ${few}`;
  }
  return `${count} ${many}`;
}

export interface Messages {
  common: {
    appName: string;
    loading: string;
    refreshing: string;
    retry: string;
    close: string;
    cancel: string;
    apply: string;
    reset: string;
    clear: string;
    unknown: string;
  };
  app: {
    title: string;
    subtitle: string;
    configuration: string;
    refresh: string;
    syncSource: string;
    syncing: string;
    language: string;
    timeZone: string;
    localZone: (zone: string) => string;
    utc: string;
  };
  tabs: {
    label: string;
    problems: string;
    recent: string;
    history: string;
    problemsHint: string;
    recentHint: string;
    historyHint: string;
  };
  range: {
    label: string;
    oneHour: string;
    twentyFourHours: string;
    sevenDays: string;
    thirtyDays: string;
    custom: string;
    from: string;
    to: string;
    apply: string;
    invalidOrder: string;
    invalidDate: string;
    tooLong: string;
    window: (from: string, to: string) => string;
    rolling: string;
  };
  filters: {
    label: string;
    search: string;
    searchPlaceholder: string;
    severity: string;
    severityAll: string;
    host: string;
    hostPlaceholder: string;
    assignee: string;
    assigneePlaceholder: string;
    assignment: string;
    assignmentAll: string;
    assigned: string;
    unassigned: string;
    ack: string;
    ackAll: string;
    acknowledged: string;
    unacknowledged: string;
    state: string;
    stateAll: string;
    stateActive: string;
    stateRecovered: string;
    sort: string;
    sortSeverity: string;
    sortStarted: string;
    sortDuration: string;
    sortRecovered: string;
    order: string;
    orderAsc: string;
    orderDesc: string;
    labels: string;
    labelKey: string;
    labelKeyPlaceholder: string;
    labelValue: string;
    labelValuePlaceholder: string;
    labelPairHint: string;
    labelPairError: string;
    apply: string;
    reset: string;
    activeCount: (count: number) => string;
  };
  summary: {
    matching: (count: number) => string;
    matchingLabel: string;
    active: string;
    recovered: string;
    acknowledged: string;
    unacknowledged: string;
    unassigned: string;
    critical: string;
    warning: string;
  };
  table: {
    caption: string;
    severity: string;
    problem: string;
    status: string;
    started: string;
    duration: string;
    owner: string;
    ack: string;
    open: (name: string) => string;
    recoveredAt: (time: string) => string;
    noTarget: string;
    emptyTarget: string;
    untitled: string;
  };
  status: {
    active: string;
    recovered: string;
  };
  severity: {
    critical: string;
    warning: string;
    info: string;
    unknown: string;
  };
  ack: {
    acknowledged: string;
    unacknowledged: string;
    acknowledgedBy: (name: string) => string;
    notAcknowledged: string;
  };
  pagination: {
    previous: string;
    next: string;
    range: (from: number, to: number, total: number) => string;
    pageSize: string;
    page: (current: number, total: number) => string;
  };
  source: {
    lastSuccess: (value: string) => string;
    never: string;
    staleTitle: string;
    staleBody: string;
    updated: (value: string) => string;
  };
  errors: {
    forbidden: string;
    missing: string;
    invalid: string;
    unavailable: string;
    listTitle: string;
    actionTitle: string;
    detailTitle: string;
    syncTitle: string;
  };
  deepLink: {
    resolving: string;
    notFoundTitle: string;
    notFoundBody: string;
    notFoundBodyStart: (start: string) => string;
    ambiguousTitle: string;
    ambiguousBody: string;
    invalidTitle: string;
    invalidFingerprintBody: string;
    invalidMatchersBody: string;
    errorTitle: string;
    errorBody: string;
    retry: string;
  };
  empty: {
    problemsTitle: string;
    problemsBody: string;
    recentTitle: string;
    recentBody: string;
    historyTitle: string;
    historyBody: string;
    resetFilters: string;
  };
  drawer: {
    title: string;
    close: string;
    sourceRule: string;
    copyLink: string;
    copyDone: string;
    copyFailed: string;
    description: string;
    noDescription: string;
    labels: string;
    noLabels: string;
    annotations: string;
    noAnnotations: string;
    facts: {
      state: string;
      started: string;
      recovered: string;
      duration: string;
      owner: string;
      ack: string;
      ackAt: string;
      episode: string;
    };
    owner: string;
    ownerPlaceholder: string;
    ownerHint: string;
    saveOwner: string;
    saving: string;
    acknowledge: string;
    unacknowledge: string;
    ackHint: string;
    viewer: string;
    discardConfirm: string;
    activity: string;
    activityNewest: string;
    noActivity: string;
    comments: string;
    noComments: string;
    commentPlaceholder: string;
    addComment: string;
    commentLimit: (used: number, max: number) => string;
  };
  activity: {
    acknowledged: string;
    unacknowledged: string;
    ownerSet: (name: string) => string;
    ownerCleared: string;
    comment: string;
    unknownKind: (kind: string) => string;
  };
  csv: {
    exportPage: string;
  };
  config: {
    title: string;
    warningTitle: string;
    warningBody: string;
    sourceSection: string;
    grafanaUrl: string;
    grafanaUrlHelp: string;
    token: string;
    tokenHelp: string;
    tokenConfigured: string;
    tokenPlaceholder: string;
    interval: string;
    intervalHelp: string;
    confirm: string;
    confirmLabel: string;
    save: string;
    saving: string;
    errorTitle: string;
    requiredToken: string;
    requiredURL: string;
  };
}

const en: Messages = {
  common: {
    appName: 'Problem Desk',
    loading: 'Loading…',
    refreshing: 'Refreshing…',
    retry: 'Retry',
    close: 'Close',
    cancel: 'Cancel',
    apply: 'Apply',
    reset: 'Reset',
    clear: 'Clear',
    unknown: 'Unknown',
  },
  app: {
    title: 'Problem Desk',
    subtitle: 'Alert episodes and operator history',
    configuration: 'Configuration',
    refresh: 'Refresh',
    syncSource: 'Sync source',
    syncing: 'Syncing…',
    language: 'Language',
    timeZone: 'Time zone',
    localZone: (zone) => `Local (${zone})`,
    utc: 'UTC',
  },
  tabs: {
    label: 'Problem views',
    problems: 'Problems',
    recent: 'Recent',
    history: 'History',
    problemsHint: 'All firing episodes, regardless of age',
    recentHint: 'Episodes active or recovered in the selected window',
    historyHint: 'Episodes overlapping the selected window',
  },
  range: {
    label: 'Window',
    oneHour: '1h',
    twentyFourHours: '24h',
    sevenDays: '7d',
    thirtyDays: '30d',
    custom: 'Custom',
    from: 'From',
    to: 'To',
    apply: 'Apply range',
    invalidOrder: 'The start must be earlier than the end.',
    invalidDate: 'Enter a valid start and end date.',
    tooLong: 'The range must not exceed 366 days.',
    window: (from, to) => `${from} to ${to}`,
    rolling: 'Rolling window; refreshed on each load.',
  },
  filters: {
    label: 'Filters',
    search: 'Search',
    searchPlaceholder: 'Rule, host, label value…',
    severity: 'Severity',
    severityAll: 'All severities',
    host: 'Host',
    hostPlaceholder: 'Host contains…',
    assignee: 'Owner',
    assigneePlaceholder: 'Exact login or team',
    assignment: 'Assignment',
    assignmentAll: 'Any owner',
    assigned: 'Assigned',
    unassigned: 'Unassigned',
    ack: 'Acknowledgement',
    ackAll: 'Any acknowledgement',
    acknowledged: 'Acknowledged',
    unacknowledged: 'Unacknowledged',
    state: 'State',
    stateAll: 'Active and recovered',
    stateActive: 'Active',
    stateRecovered: 'Recovered',
    sort: 'Sort',
    sortSeverity: 'Severity',
    sortStarted: 'Started',
    sortDuration: 'Duration',
    sortRecovered: 'Recovered',
    order: 'Order',
    orderAsc: 'Ascending',
    orderDesc: 'Descending',
    labels: 'Labels',
    labelKey: 'Label',
    labelKeyPlaceholder: 'Select label',
    labelValue: 'Value',
    labelValuePlaceholder: 'Exact value',
    labelPairHint: 'Both label and value are required.',
    labelPairError: 'Enter both a label and its value.',
    apply: 'Apply filters',
    reset: 'Reset filters',
    activeCount: (count) => `${count} active ${count === 1 ? 'filter' : 'filters'}`,
  },
  summary: {
    matching: (count) => enCount(count, 'episode', 'episodes'),
    matchingLabel: 'Matching',
    active: 'Active',
    recovered: 'Recovered',
    acknowledged: 'Acknowledged',
    unacknowledged: 'Unacknowledged',
    unassigned: 'Unassigned',
    critical: 'Critical',
    warning: 'Warning',
  },
  table: {
    caption: 'Alert episodes',
    severity: 'Severity',
    problem: 'Problem',
    status: 'Status',
    started: 'Started',
    duration: 'Duration',
    owner: 'Owner',
    ack: 'Acknowledgement',
    open: (name) => `Open episode ${name}`,
    recoveredAt: (time) => `Recovered ${time}`,
    noTarget: 'Target not specified',
    emptyTarget: 'Target not specified',
    untitled: 'Untitled problem',
  },
  status: {
    active: 'Active',
    recovered: 'Recovered',
  },
  severity: {
    critical: 'Critical',
    warning: 'Warning',
    info: 'Info',
    unknown: 'Unspecified',
  },
  ack: {
    acknowledged: 'Acknowledged',
    unacknowledged: 'Unacknowledged',
    acknowledgedBy: (name) => `Acknowledged by ${name}`,
    notAcknowledged: 'Not acknowledged',
  },
  pagination: {
    previous: 'Previous',
    next: 'Next',
    range: (from, to, total) => `${from}–${to} of ${total}`,
    pageSize: 'Page size',
    page: (current, total) => `Page ${current} of ${total}`,
  },
  source: {
    lastSuccess: (value) => `Last source sync: ${value}`,
    never: 'No source sync recorded',
    staleTitle: 'Source unavailable — states may be stale',
    staleBody: 'The Grafana Alerting sync failed. Active problems stay active until the source responds.',
    updated: (value) => `Updated ${value}`,
  },
  errors: {
    forbidden: 'Your Grafana role does not permit this action.',
    missing: 'This episode is not available in your organization.',
    invalid: 'The request was rejected. Check the entered values.',
    unavailable: 'The request failed. Try again or check the plugin configuration.',
    listTitle: 'Could not load problems',
    actionTitle: 'Action failed',
    detailTitle: 'Could not load the episode',
    syncTitle: 'Sync failed',
  },
  deepLink: {
    resolving: 'Opening the linked episode…',
    notFoundTitle: 'Episode not observed yet',
    notFoundBody: 'The alert episode is not in the store yet. Ingestion may still be in progress. Retry in a moment.',
    notFoundBodyStart: (start) => `No episode is recorded for this link yet (start ${start}). Ingestion may still be in progress.`,
    ambiguousTitle: 'Linked episode is ambiguous',
    ambiguousBody: 'More than one episode matches this link. Open the required episode from the list.',
    invalidTitle: 'Invalid episode link',
    invalidFingerprintBody: 'The link must contain both the alert fingerprint and its start time.',
    invalidMatchersBody: 'The link does not contain valid alert matchers.',
    errorTitle: 'Could not resolve the linked episode',
    errorBody: 'The lookup failed. Retry or open the episode from the list.',
    retry: 'Retry lookup',
  },
  empty: {
    problemsTitle: 'No firing episodes',
    problemsBody: 'Only currently firing alerts are collected. Change the filters or wait for the next source sync.',
    recentTitle: 'No recent episodes',
    recentBody: 'No episode was active or recovered in this window. Widen the window or change the filters.',
    historyTitle: 'No episode history',
    historyBody: 'No episode overlaps this window. Widen the window or change the filters.',
    resetFilters: 'Reset filters',
  },
  drawer: {
    title: 'Episode details',
    close: 'Close episode details',
    sourceRule: 'Open source rule',
    copyLink: 'Copy app link',
    copyDone: 'Link copied',
    copyFailed: 'Could not copy the link.',
    description: 'Description',
    noDescription: 'No description provided.',
    labels: 'Labels',
    noLabels: 'No labels.',
    annotations: 'Annotations',
    noAnnotations: 'No annotations.',
    facts: {
      state: 'State',
      started: 'Started',
      recovered: 'Recovered',
      duration: 'Duration',
      owner: 'Owner',
      ack: 'Acknowledgement',
      ackAt: 'Acknowledged at',
      episode: 'Episode',
    },
    owner: 'Owner label',
    ownerPlaceholder: 'Login or team name',
    ownerHint: 'The owner label does not grant permissions.',
    saveOwner: 'Save owner',
    saving: 'Saving…',
    acknowledge: 'Acknowledge',
    unacknowledge: 'Remove acknowledgement',
    ackHint: 'Acknowledgement does not mute notifications or resolve the alert.',
    viewer: 'Viewer access is read-only. Editors and administrators can comment and acknowledge.',
    discardConfirm: 'Discard your unsent comment?',
    activity: 'Activity',
    activityNewest: 'Newest first',
    noActivity: 'No operator actions recorded.',
    comments: 'Discussion',
    noComments: 'No comments on this episode yet.',
    commentPlaceholder: 'What did you investigate or change?',
    addComment: 'Add comment',
    commentLimit: (used, max) => `${used} / ${max}`,
  },
  activity: {
    acknowledged: 'Acknowledged',
    unacknowledged: 'Acknowledgement removed',
    ownerSet: (name) => `Owner set to ${name}`,
    ownerCleared: 'Owner cleared',
    comment: 'Comment',
    unknownKind: (kind) => `Event: ${kind}`,
  },
  csv: {
    exportPage: 'Export page CSV',
  },
  config: {
    title: 'Connect this organization',
    warningTitle: 'Organization-wide visibility',
    warningBody:
      "Problem Desk shares the source token's alert data with all authenticated members of this organization. Grafana alert folder permissions are not reproduced here. Do not enable it if the token can read alerts that these members must not see.",
    sourceSection: 'Grafana source',
    grafanaUrl: 'Grafana URL',
    grafanaUrlHelp: 'The URL reachable by the plugin backend. The token must belong to this same organization.',
    token: 'Grafana service account token',
    tokenHelp: 'Stored in Grafana secure settings. Never enter a token from another organization.',
    tokenConfigured: 'A token is configured',
    tokenPlaceholder: 'Enter a service account token',
    interval: 'Polling interval (seconds)',
    intervalHelp: 'From 10 to 3600 seconds.',
    confirm: 'Confirm sharing scope',
    confirmLabel: 'I understand that this is an organization-shared workspace.',
    save: 'Save and enable',
    saving: 'Saving…',
    errorTitle: 'Configuration not saved',
    requiredToken: 'Enter a service account token.',
    requiredURL: 'Enter the Grafana URL.',
  },
};

const ru: Messages = {
  common: {
    appName: 'Problem Desk',
    loading: 'Загрузка…',
    refreshing: 'Обновление…',
    retry: 'Повторить',
    close: 'Закрыть',
    cancel: 'Отмена',
    apply: 'Применить',
    reset: 'Сбросить',
    clear: 'Очистить',
    unknown: 'Неизвестно',
  },
  app: {
    title: 'Problem Desk',
    subtitle: 'Проблемы и журнал действий',
    configuration: 'Настройка',
    refresh: 'Обновить',
    syncSource: 'Синхронизировать источник',
    syncing: 'Синхронизация…',
    language: 'Язык',
    timeZone: 'Часовой пояс',
    localZone: (zone) => `Локальный (${zone})`,
    utc: 'UTC',
  },
  tabs: {
    label: 'Представления проблем',
    problems: 'Проблемы',
    recent: 'Недавние',
    history: 'История',
    problemsHint: 'Все горящие эпизоды без ограничения по возрасту',
    recentHint: 'Эпизоды, активные или восстановленные в выбранном окне',
    historyHint: 'Эпизоды, пересекающиеся с выбранным окном',
  },
  range: {
    label: 'Окно',
    oneHour: '1 ч',
    twentyFourHours: '24 ч',
    sevenDays: '7 дн',
    thirtyDays: '30 дн',
    custom: 'Период',
    from: 'С',
    to: 'По',
    apply: 'Применить период',
    invalidOrder: 'Начало должно быть раньше конца.',
    invalidDate: 'Укажите корректные начало и конец периода.',
    tooLong: 'Период не должен превышать 366 дней.',
    window: (from, to) => `${from} — ${to}`,
    rolling: 'Скользящее окно; обновляется при каждой загрузке.',
  },
  filters: {
    label: 'Фильтры',
    search: 'Поиск',
    searchPlaceholder: 'Правило, хост, значение метки…',
    severity: 'Важность',
    severityAll: 'Любая важность',
    host: 'Хост',
    hostPlaceholder: 'Хост содержит…',
    assignee: 'Ответственный',
    assigneePlaceholder: 'Точный логин или команда',
    assignment: 'Назначение',
    assignmentAll: 'Любой ответственный',
    assigned: 'Назначено',
    unassigned: 'Не назначено',
    ack: 'Подтверждение',
    ackAll: 'Любое подтверждение',
    acknowledged: 'Подтверждено',
    unacknowledged: 'Без подтверждения',
    state: 'Состояние',
    stateAll: 'Активные и восстановленные',
    stateActive: 'Активные',
    stateRecovered: 'Восстановленные',
    sort: 'Сортировка',
    sortSeverity: 'Важность',
    sortStarted: 'Начало',
    sortDuration: 'Длительность',
    sortRecovered: 'Восстановление',
    order: 'Порядок',
    orderAsc: 'По возрастанию',
    orderDesc: 'По убыванию',
    labels: 'Метки',
    labelKey: 'Метка',
    labelKeyPlaceholder: 'Выберите метку',
    labelValue: 'Значение',
    labelValuePlaceholder: 'Точное значение',
    labelPairHint: 'Нужны и метка, и значение.',
    labelPairError: 'Укажите метку и её значение.',
    apply: 'Применить фильтры',
    reset: 'Сбросить фильтры',
    activeCount: (count) => `${ruCount(count, 'активный фильтр', 'активных фильтра', 'активных фильтров')}`,
  },
  summary: {
    matching: (count) => ruCount(count, 'эпизод', 'эпизода', 'эпизодов'),
    matchingLabel: 'Найдено',
    active: 'Активные',
    recovered: 'Восстановленные',
    acknowledged: 'Подтверждённые',
    unacknowledged: 'Без подтверждения',
    unassigned: 'Без владельца',
    critical: 'Критические',
    warning: 'Предупреждения',
  },
  table: {
    caption: 'Эпизоды алертов',
    severity: 'Важность',
    problem: 'Проблема',
    status: 'Состояние',
    started: 'Начало',
    duration: 'Длительность',
    owner: 'Ответственный',
    ack: 'Подтверждение',
    open: (name) => `Открыть эпизод ${name}`,
    recoveredAt: (time) => `Восстановлен ${time}`,
    noTarget: 'Цель не указана',
    emptyTarget: 'Цель не указана',
    untitled: 'Проблема без названия',
  },
  status: {
    active: 'Активна',
    recovered: 'Восстановлена',
  },
  severity: {
    critical: 'Критический',
    warning: 'Предупреждение',
    info: 'Информация',
    unknown: 'Не указана',
  },
  ack: {
    acknowledged: 'Подтверждено',
    unacknowledged: 'Не подтверждено',
    acknowledgedBy: (name) => `Подтвердил(а): ${name}`,
    notAcknowledged: 'Без подтверждения',
  },
  pagination: {
    previous: 'Назад',
    next: 'Вперёд',
    range: (from, to, total) => `${from}–${to} из ${total}`,
    pageSize: 'Строк на странице',
    page: (current, total) => `Страница ${current} из ${total}`,
  },
  source: {
    lastSuccess: (value) => `Последняя синхронизация: ${value}`,
    never: 'Синхронизаций ещё не было',
    staleTitle: 'Источник недоступен — состояния могут быть устаревшими',
    staleBody: 'Синхронизация с Grafana Alerting не удалась. Активные проблемы останутся активными, пока источник не ответит.',
    updated: (value) => `Обновлено: ${value}`,
  },
  errors: {
    forbidden: 'Ваша роль Grafana не разрешает это действие.',
    missing: 'Эпизод недоступен в этой организации.',
    invalid: 'Запрос отклонён. Проверьте введённые значения.',
    unavailable: 'Запрос не выполнен. Повторите попытку или проверьте настройку плагина.',
    listTitle: 'Не удалось загрузить проблемы',
    actionTitle: 'Действие не выполнено',
    detailTitle: 'Не удалось загрузить эпизод',
    syncTitle: 'Синхронизация не выполнена',
  },
  deepLink: {
    resolving: 'Открываем эпизод по ссылке…',
    notFoundTitle: 'Эпизод ещё не наблюдался',
    notFoundBody: 'Эпизода пока нет в хранилище. Возможно, приём алертов ещё идёт. Повторите попытку через минуту.',
    notFoundBodyStart: (start) => `Для этой ссылки ещё нет эпизода (начало ${start}). Возможно, приём алертов ещё идёт.`,
    ambiguousTitle: 'Ссылка указывает на несколько эпизодов',
    ambiguousBody: 'Ссылке соответствует больше одного эпизода. Откройте нужный эпизод из списка.',
    invalidTitle: 'Некорректная ссылка на эпизод',
    invalidFingerprintBody: 'В ссылке должны быть отпечаток алерта и время начала.',
    invalidMatchersBody: 'В ссылке нет корректных меток алерта.',
    errorTitle: 'Не удалось найти эпизод по ссылке',
    errorBody: 'Поиск не выполнен. Повторите попытку или откройте эпизод из списка.',
    retry: 'Повторить поиск',
  },
  empty: {
    problemsTitle: 'Нет горящих эпизодов',
    problemsBody: 'Собираются только горящие алерты. Измените фильтры или дождитесь следующей синхронизации.',
    recentTitle: 'Нет недавних эпизодов',
    recentBody: 'В этом окне нет активных или восстановленных эпизодов. Расширьте окно или измените фильтры.',
    historyTitle: 'Нет истории эпизодов',
    historyBody: 'Ни один эпизод не пересекается с этим окном. Расширьте окно или измените фильтры.',
    resetFilters: 'Сбросить фильтры',
  },
  drawer: {
    title: 'Детали эпизода',
    close: 'Закрыть детали эпизода',
    sourceRule: 'Открыть исходное правило',
    copyLink: 'Скопировать ссылку',
    copyDone: 'Ссылка скопирована',
    copyFailed: 'Не удалось скопировать ссылку.',
    description: 'Описание',
    noDescription: 'Описание не задано.',
    labels: 'Метки',
    noLabels: 'Меток нет.',
    annotations: 'Аннотации',
    noAnnotations: 'Аннотаций нет.',
    facts: {
      state: 'Состояние',
      started: 'Начало',
      recovered: 'Восстановлен',
      duration: 'Длительность',
      owner: 'Ответственный',
      ack: 'Подтверждение',
      ackAt: 'Подтверждён',
      episode: 'Эпизод',
    },
    owner: 'Метка ответственного',
    ownerPlaceholder: 'Логин или название команды',
    ownerHint: 'Метка ответственного не выдаёт прав доступа.',
    saveOwner: 'Сохранить ответственного',
    saving: 'Сохранение…',
    acknowledge: 'Подтвердить',
    unacknowledge: 'Снять подтверждение',
    ackHint: 'Подтверждение не отключает уведомления и не закрывает алерт.',
    viewer: 'Роль Viewer: только просмотр. Редакторы и администраторы могут комментировать и подтверждать.',
    discardConfirm: 'Отменить неотправленный комментарий?',
    activity: 'Активность',
    activityNewest: 'Сначала новые',
    noActivity: 'Действий операторов пока нет.',
    comments: 'Обсуждение',
    noComments: 'Комментариев к этому эпизоду пока нет.',
    commentPlaceholder: 'Что вы проверили или изменили?',
    addComment: 'Добавить комментарий',
    commentLimit: (used, max) => `${used} / ${max}`,
  },
  activity: {
    acknowledged: 'Подтверждено',
    unacknowledged: 'Подтверждение снято',
    ownerSet: (name) => `Ответственный: ${name}`,
    ownerCleared: 'Ответственный снят',
    comment: 'Комментарий',
    unknownKind: (kind) => `Событие: ${kind}`,
  },
  csv: {
    exportPage: 'Экспорт страницы в CSV',
  },
  config: {
    title: 'Подключение организации',
    warningTitle: 'Видимость для всей организации',
    warningBody:
      'Problem Desk открывает данные алертов, доступные токену источника, всем авторизованным участникам этой организации. Права Grafana на папки алертов здесь не воспроизводятся. Не включайте плагин, если токен видит алерты, которые этим участникам видеть нельзя.',
    sourceSection: 'Источник Grafana',
    grafanaUrl: 'URL Grafana',
    grafanaUrlHelp: 'URL, доступный бэкенду плагина. Токен должен принадлежать этой же организации.',
    token: 'Токен сервисной учётной записи Grafana',
    tokenHelp: 'Хранится в защищённых настройках Grafana. Не вводите токен другой организации.',
    tokenConfigured: 'Токен настроен',
    tokenPlaceholder: 'Введите токен сервисной учётной записи',
    interval: 'Интервал опроса (секунды)',
    intervalHelp: 'От 10 до 3600 секунд.',
    confirm: 'Подтверждение области доступа',
    confirmLabel: 'Я понимаю, что это рабочее пространство доступно всей организации.',
    save: 'Сохранить и включить',
    saving: 'Сохранение…',
    errorTitle: 'Настройки не сохранены',
    requiredToken: 'Введите токен сервисной учётной записи.',
    requiredURL: 'Укажите URL Grafana.',
  },
};

export const dictionaries: Record<Language, Messages> = { en, ru };

export function resolveLanguage(candidates: Array<string | undefined | null>): Language {
  for (const candidate of candidates) {
    if (candidate && /^ru(?:[-_]|$)/i.test(candidate.trim())) {
      return 'ru';
    }
  }
  return 'en';
}
