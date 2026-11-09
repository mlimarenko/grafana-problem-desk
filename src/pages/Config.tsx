import React, { useState } from 'react';
import { AppPluginMeta, PluginConfigPageProps } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { Alert, Button, Field, Input, SecretInput } from '@grafana/ui';
import { Options, describeError, pluginId } from '../model';

export function Config({ plugin }: PluginConfigPageProps<AppPluginMeta<Options>>) {
  const meta = plugin.meta;
  const [url, setURL] = useState(meta.jsonData?.grafanaUrl || 'http://localhost:3000');
  const [interval, setInterval] = useState(meta.jsonData?.pollIntervalSeconds || 30);
  const [token, setToken] = useState('');
  const [configured, setConfigured] = useState(Boolean(meta.secureJsonFields?.grafanaToken));
  const [accepted, setAccepted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function save(event: React.FormEvent) {
    event.preventDefault(); setBusy(true); setError('');
    try {
      await getBackendSrv().post(`/api/plugins/${pluginId}/settings`, {
        enabled: true, pinned: true,
        jsonData: { grafanaUrl: url.trim(), pollIntervalSeconds: interval },
        ...(configured ? {} : { secureJsonData: { grafanaToken: token } }),
      });
      window.location.reload();
    } catch (error) { setError(describeError(error)); } finally { setBusy(false); }
  }

  return <section style={{ maxWidth: 760, padding: 24 }}>
    <h2>Connect this organization</h2>
    <Alert title="Organization-wide visibility" severity="warning">
      Problem Desk shares the source token&apos;s alert data with all authenticated members of this organization.
      Grafana alert folder permissions are not reproduced here. Do not enable it if the token can read alerts that these members must not see.
    </Alert>
    <form onSubmit={save}>
      <Field label="Grafana URL" description="The URL reachable by the plugin backend. The token must belong to this same organization.">
        <Input id="source-url" aria-label="Grafana URL" type="url" required value={url} onChange={e => setURL(e.currentTarget.value)} />
      </Field>
      <Field label="Grafana service account token" description="Stored in Grafana secure settings. Never enter a token from another organization.">
        <SecretInput id="source-token" aria-label="Grafana service account token" value={token} isConfigured={configured}
          onChange={e => setToken(e.currentTarget.value)} onReset={() => { setConfigured(false); setToken(''); }} />
      </Field>
      <Field label="Polling interval (seconds)">
        <Input id="source-interval" aria-label="Polling interval in seconds" type="number" min={10} max={3600} required value={interval}
          onChange={e => setInterval(Number(e.currentTarget.value))} />
      </Field>
      <Field label="Confirm sharing scope">
        <label><input type="checkbox" checked={accepted} onChange={e => setAccepted(e.currentTarget.checked)} /> I understand that this is an organization-shared workspace.</label>
      </Field>
      {error && <Alert title="Configuration not saved" severity="error">{error}</Alert>}
      <Button type="submit" disabled={busy || !accepted || !url.trim() || (!configured && !token.trim())}>{busy ? 'Saving…' : 'Save and enable'}</Button>
    </form>
  </section>;
}
