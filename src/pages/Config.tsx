import React, { useState } from 'react';
import { css } from '@emotion/css';
import type { AppPluginMeta, GrafanaTheme2, PluginConfigPageProps } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { Alert, Button, Field, Input, SecretInput, Select, useStyles2 } from '@grafana/ui';
import { classifyError, pluginId, type Options } from '../model';
import { errorText, I18nProvider, useI18n, type Language } from '../i18n';

export function Config(props: PluginConfigPageProps<AppPluginMeta<Options>>) {
  return (
    <I18nProvider>
      <ConfigPage {...props} />
    </I18nProvider>
  );
}

function ConfigPage({ plugin }: PluginConfigPageProps<AppPluginMeta<Options>>) {
  const styles = useStyles2(getStyles);
  const { language, setLanguage, messages: m } = useI18n();
  const meta = plugin.meta;
  const [url, setURL] = useState(meta.jsonData?.grafanaUrl || 'http://localhost:3000');
  const [interval, setInterval] = useState(meta.jsonData?.pollIntervalSeconds || 30);
  const [token, setToken] = useState('');
  const [configured, setConfigured] = useState(Boolean(meta.secureJsonFields?.grafanaToken));
  const [accepted, setAccepted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const validInterval = Number.isInteger(interval) && interval >= 10 && interval <= 3600;
  const canSave = accepted && Boolean(url.trim()) && validInterval && (configured || Boolean(token.trim()));

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!url.trim()) {
      setError(m.config.requiredURL);
      return;
    }
    if (!configured && !token.trim()) {
      setError(m.config.requiredToken);
      return;
    }
    if (!validInterval) {
      setError(m.config.intervalHelp);
      return;
    }
    setBusy(true);
    setError('');
    try {
      await getBackendSrv().post(`/api/plugins/${pluginId}/settings`, {
        enabled: true,
        pinned: true,
        jsonData: { grafanaUrl: url.trim(), pollIntervalSeconds: interval },
        ...(configured ? {} : { secureJsonData: { grafanaToken: token } }),
      });
      window.location.reload();
    } catch (requestError) {
      setError(errorText(classifyError(requestError), m));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className={styles.page}>
      <header className={styles.header}>
        <div>
          <h2 className={styles.title}>{m.config.title}</h2>
        </div>
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
      </header>

      <Alert title={m.config.warningTitle} severity="warning">
        {m.config.warningBody}
      </Alert>

      <form className={styles.form} onSubmit={save}>
        <section className={styles.section} aria-labelledby="config-source-heading">
          <h3 id="config-source-heading" className={styles.sectionTitle}>
            {m.config.sourceSection}
          </h3>
          <Field label={m.config.grafanaUrl} description={m.config.grafanaUrlHelp}>
            <Input
              id="source-url"
              aria-label={m.config.grafanaUrl}
              type="url"
              required
              value={url}
              onChange={(event) => setURL(event.currentTarget.value)}
            />
          </Field>
          <Field label={m.config.token} description={m.config.tokenHelp}>
            <SecretInput
              id="source-token"
              aria-label={m.config.token}
              value={token}
              isConfigured={configured}
              placeholder={m.config.tokenPlaceholder}
              onChange={(event) => setToken(event.currentTarget.value)}
              onReset={() => {
                setConfigured(false);
                setToken('');
              }}
            />
          </Field>
          {configured && <p className={styles.configured}>{m.config.tokenConfigured}</p>}
          <Field label={m.config.interval} description={m.config.intervalHelp}>
            <Input
              id="source-interval"
              aria-label={m.config.interval}
              type="number"
              min={10}
              max={3600}
              required
              value={interval}
              onChange={(event) => setInterval(Number(event.currentTarget.value))}
            />
          </Field>
        </section>

        <section className={styles.section} aria-labelledby="config-scope-heading">
          <h3 id="config-scope-heading" className={styles.sectionTitle}>
            {m.config.confirm}
          </h3>
          <label className={styles.checkbox}>
            <input type="checkbox" checked={accepted} onChange={(event) => setAccepted(event.currentTarget.checked)} />
            <span>{m.config.confirmLabel}</span>
          </label>
        </section>

        {error && (
          <Alert title={m.config.errorTitle} severity="error">
            {error}
          </Alert>
        )}

        <Button type="submit" disabled={busy || !canSave}>
          {busy ? m.config.saving : m.config.save}
        </Button>
      </form>
    </main>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  page: css`
    box-sizing: border-box;
    width: 100%;
    max-width: 880px;
    padding: ${theme.spacing(2.5)};
  `,
  header: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: ${theme.spacing(2)};
    flex-wrap: wrap;
    margin-bottom: ${theme.spacing(2)};
  `,
  title: css`
    margin: 0;
  `,
  form: css`
    display: flex;
    flex-direction: column;
    gap: ${theme.spacing(2)};
    margin-top: ${theme.spacing(2)};
  `,
  section: css`
    padding: ${theme.spacing(2)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.canvas};
  `,
  sectionTitle: css`
    margin: 0 0 ${theme.spacing(1.5)};
    font-size: ${theme.typography.h5.fontSize};
  `,
  configured: css`
    margin-top: -${theme.spacing(1)};
    color: ${theme.colors.success.text};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
  checkbox: css`
    display: flex;
    align-items: flex-start;
    gap: ${theme.spacing(1)};
    cursor: pointer;
  `,
});
