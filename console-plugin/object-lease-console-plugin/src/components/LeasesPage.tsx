import * as React from 'react';
import { PageSection, Title } from '@patternfly/react-core';
import { ResourceLink, Timestamp, useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk';

// K8s Lease model (coordination.k8s.io/v1)
const leaseModel = {
  apiGroup: 'coordination.k8s.io',
  apiVersion: 'v1',
  kind: 'Lease',
  plural: 'leases',
};

const LeaseRow: React.FC<{ obj: any }> = ({ obj }) => {
  const lease = obj;
  const time = lease?.spec?.renewTime || lease?.spec?.acquireTime || lease?.metadata?.creationTimestamp;
  return (
    <tr>
      <td>
        <ResourceLink groupVersionKind={{ group: leaseModel.apiGroup, version: leaseModel.apiVersion, kind: leaseModel.kind }}
          name={lease.metadata.name} namespace={lease.metadata.namespace} />
      </td>
      <td>{lease.spec?.holderIdentity || '-'}</td>
      <td>{lease.spec?.leaseDurationSeconds ?? '-'}</td>
      <td>{lease.spec?.leaseTransitions ?? '-'}</td>
      <td><Timestamp timestamp={time} /></td>
    </tr>
  );
};

const LeasesPage: React.FC = () => {
  const [leases, loaded, loadError] = useK8sWatchResource<any[]>({
    groupVersionKind: { group: leaseModel.apiGroup, version: leaseModel.apiVersion, kind: leaseModel.kind },
    isList: true,
  namespaced: true,
  namespace: '',
  });

  return (
    <PageSection variant="light">
      <Title headingLevel="h1">Leases</Title>
      {!loaded && !loadError && <div>Loading…</div>}
      {loadError && <div>Error loading leases: {String(loadError)}</div>}
      {loaded && (
        <table className="pf-v5-c-table pf-m-grid-md" role="grid" aria-label="Leases table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Holder</th>
              <th>Duration (s)</th>
              <th>Transitions</th>
              <th>Time</th>
            </tr>
          </thead>
          <tbody>
            {leases?.map((l) => (
              <LeaseRow key={`${l.metadata.namespace || 'cluster'}-${l.metadata.name}`} obj={l} />
            ))}
          </tbody>
        </table>
      )}
    </PageSection>
  );
};

export default LeasesPage;
