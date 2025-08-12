import * as React from 'react';
import { PageSection, Title } from '@patternfly/react-core';
import { ResourceLink, Timestamp, useK8sWatchResource, useK8sWatchResources, ResourceIcon } from '@openshift-console/dynamic-plugin-sdk';

const ANN_TTL = 'object-lease-controller.ullberg.io/ttl';
const ANN_EXPIRE_AT = 'object-lease-controller.ullberg.io/expire-at';
const ANN_STATUS = 'object-lease-controller.ullberg.io/lease-status';

type GVK = { group: string; version: string; kind: string };
type WatchCfg = { groupVersionKind: GVK; namespaced: boolean; isList: true; namespace?: string };

const LeasesPage = () => {
  // Try to use LeaseController CRs if present to determine which Kinds to scan
  const leaseControllerGVK: GVK = { group: 'object-lease-controller.ullberg.io', version: 'v1alpha1', kind: 'LeaseController' };
  const [leaseControllers, lcLoaded, lcError] = useK8sWatchResource({
    groupVersionKind: leaseControllerGVK,
    isList: true,
    namespaced: true,
    namespace: '',
  });

  const watches = React.useMemo(() => {
    const cfgs: Record<string, WatchCfg> = {};
    if (lcLoaded && !lcError && Array.isArray(leaseControllers) && leaseControllers.length > 0) {
      const set = new Set<string>();
      leaseControllers.forEach((lc: any) => {
        const g = lc?.spec?.group || '';
        const v = lc?.spec?.version as string | undefined;
        const rawKind = lc?.spec?.kind;
        let k: string | undefined;
        if (typeof rawKind === 'string') {
          k = rawKind;
        } else if (rawKind && typeof rawKind === 'object') {
          // Prefer explicit singular; fall back to common field names
          k = rawKind.singular || rawKind.kind || rawKind.name;
          if (!k && typeof rawKind.plural === 'string') {
            // Heuristic: drop a single trailing 's' for plural -> singular (best-effort)
            k = rawKind.plural.replace(/s$/, '');
          }
        }
        if (v && k) set.add(`${g}~${v}~${k}`);
      });
      Array.from(set).forEach((s) => {
        const [group, version, kind] = s.split('~');
        if (!version || !kind) return; // skip incomplete
        const gvk: GVK = { group, version, kind };
        // Watch both namespaced (all namespaces) and cluster-scoped for safety
        const baseKey = `${group || 'core'}~${version}~${kind}`;
        cfgs[`${baseKey}~ns`] = {
          groupVersionKind: gvk,
          isList: true,
          namespaced: true,
          namespace: '',
        };
        cfgs[`${baseKey}~cluster`] = {
          groupVersionKind: gvk,
          isList: true,
          namespaced: false,
        };
      });
    }
    return cfgs;
  }, [leaseControllers, lcLoaded, lcError]);

  const resources = useK8sWatchResources(watches as any);

  const loaded = React.useMemo(() => Object.values(resources).every((r: any) => r.loaded || r.loadError), [resources]);
  const loadError = React.useMemo(() => (Object.values(resources).find((r: any) => r.loadError) as any)?.loadError, [resources]);

  const items: Array<{ obj: any; gvk: GVK }> = React.useMemo(() => {
    const map = new Map<string, { obj: any; gvk: GVK }>();
    Object.entries(resources).forEach(([key, res]: any) => {
      if (!res?.data) return;
      const [groupPart, version, kind] = key.split('~');
      const group = groupPart === 'core' ? '' : groupPart;
      const gvk: GVK = { group, version, kind };
      (res.data as any[]).forEach((obj) => {
        const anns = obj?.metadata?.annotations || {};
        if (anns[ANN_TTL] || anns[ANN_EXPIRE_AT] || anns[ANN_STATUS]) {
          const uid = obj?.metadata?.uid;
          const dedupKey = uid
            ? `${gvk.group}|${gvk.version}|${gvk.kind}|${uid}`
            : `${gvk.group}|${gvk.version}|${gvk.kind}|${obj?.metadata?.namespace || ''}|${obj?.metadata?.name}`;
          if (!map.has(dedupKey)) {
            map.set(dedupKey, { obj, gvk });
          }
        }
      });
    });
    const out = Array.from(map.values());
    // Sort by namespace, kind, name
    out.sort((a, b) => {
      const nsA = a.obj.metadata.namespace || '';
      const nsB = b.obj.metadata.namespace || '';
      if (nsA !== nsB) return nsA.localeCompare(nsB);
      if (a.gvk.kind !== b.gvk.kind) return a.gvk.kind.localeCompare(b.gvk.kind);
      return a.obj.metadata.name.localeCompare(b.obj.metadata.name);
    });
    return out;
  }, [resources]);

  // Derive the distinct set of monitored GVKs for display
  type Monitored = { gvk: GVK; plural?: string };
  const monitored: Monitored[] = React.useMemo(() => {
    if (!lcLoaded || lcError || !Array.isArray(leaseControllers)) return [];
    const map = new Map<string, Monitored>();
    leaseControllers.forEach((lc: any) => {
      const g = lc?.spec?.group || '';
      const v = lc?.spec?.version as string | undefined;
      const rawKind = lc?.spec?.kind;
      let k: string | undefined;
      let p: string | undefined;
      if (typeof rawKind === 'string') {
        k = rawKind;
      } else if (rawKind && typeof rawKind === 'object') {
        k = rawKind.singular || rawKind.kind || rawKind.name || (typeof rawKind.plural === 'string' ? rawKind.plural.replace(/s$/, '') : undefined);
        if (typeof rawKind.plural === 'string') p = rawKind.plural.toLowerCase();
      }
      if (v && k) {
        const key = `${g}~${v}~${k}`;
        if (!map.has(key)) map.set(key, { gvk: { group: g, version: v, kind: k }, plural: p });
      }
    });
    const list = Array.from(map.values());
    list.sort((a, b) => {
      const ga = a.gvk.group || '';
      const gb = b.gvk.group || '';
      if (ga !== gb) return ga.localeCompare(gb);
      if (a.gvk.version !== b.gvk.version) return a.gvk.version.localeCompare(b.gvk.version);
      return a.gvk.kind.localeCompare(b.gvk.kind);
    });
    return list;
  }, [leaseControllers, lcLoaded, lcError]);

  return (
    <PageSection variant="default" className="pf-v5-u-m-md pf-v5-u-p-md">
      <Title headingLevel="h1">Objects with TTL annotation</Title>
      {!loaded && !loadError && <div>Loading…</div>}
      {loadError && <div>Error loading: {String(loadError?.message || loadError)}</div>}
      {lcLoaded && !lcError && Array.isArray(leaseControllers) && leaseControllers.length === 0 && (
        <div>No LeaseController resources found.</div>
      )}
      {loaded && monitored.map(({ gvk, plural }) => {
        const ref = `${gvk.group || 'core'}~${gvk.version}~${gvk.kind}`;
        const crdGVK: GVK = { group: 'apiextensions.k8s.io', version: 'v1', kind: 'CustomResourceDefinition' };
        const crdName = gvk.group && plural ? `${plural}.${gvk.group}` : undefined;
        const rows = items.filter((it) => it.gvk.group === gvk.group && it.gvk.version === gvk.version && it.gvk.kind === gvk.kind);
        return (
          <table key={ref} className="pf-v5-c-table pf-m-grid-md" role="grid" style={{ marginTop: 16 }} aria-label={`Objects for ${ref}`}>
            <caption style={{ textAlign: 'left', paddingBottom: 6 }}>
              <strong>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                  <ResourceIcon groupVersionKind={gvk} />
                  <a href={`/api-resource/all-namespaces/${ref}`}>{gvk.kind}</a>
                </span>
              </strong>
            </caption>
            {rows.length > 0 && (
              <thead>
              <tr>
                <th>Namespace</th>
                <th>Name</th>
                <th>TTL</th>
                <th>Expires</th>
                <th>Status</th>
              </tr>
              </thead>
            )}
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={5}>No resources with lease annotations found for this GVK.</td>
                </tr>
              ) : (
                rows.map(({ obj }) => {
                  const key = `${obj.metadata.namespace || 'cluster'}-${obj.metadata.name}`;
                  return (
                    <tr key={key}>
                      <td>{obj.metadata.namespace || '-'}</td>
                      <td>
                        <ResourceLink groupVersionKind={gvk} name={obj.metadata.name} namespace={obj.metadata.namespace} />
                      </td>
                      <td>{obj?.metadata?.annotations?.[ANN_TTL] ?? '-'}</td>
                      <td><Timestamp timestamp={obj?.metadata?.annotations?.[ANN_EXPIRE_AT]} /></td>
                      <td>{obj?.metadata?.annotations?.[ANN_STATUS] ?? '-'}</td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        );
      })}
    </PageSection>
  );
};

export default LeasesPage;
