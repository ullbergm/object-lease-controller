// Minimal shims so the editor doesn't complain when building outside the container.
declare module '@openshift-console/dynamic-plugin-sdk' {
  export const ResourceLink: any;
  export const ResourceIcon: any;
  export const Timestamp: any;
  export const useK8sModels: any;
  export const useK8sWatchResources: any;
  export const useK8sWatchResource: any;
}

declare module '@patternfly/react-core' {
  export const PageSection: any;
  export const Title: any;
}

declare module 'react' {
  const React: any;
  export default React;
  export = React;
}

declare module 'react/jsx-runtime' {
  export const jsx: any;
  export const jsxs: any;
  export const Fragment: any;
}

// Ensure JSX types exist
declare namespace JSX {
  interface IntrinsicElements {
    [elemName: string]: any;
  }
}
