// Minimal shims so the editor doesn't complain when building outside the container.

// React types - defined first to avoid circular imports
declare module 'react' {
  export type ReactNode = string | number | boolean | null | undefined | ReactElement | ReactNode[];
  export type ReactElement = { type: unknown; props: unknown; key: string | number | null };
  export type ComponentType<P = Record<string, unknown>> = (props: P) => ReactElement | null;
  export type FC<P = Record<string, unknown>> = ComponentType<P>;

  export function useMemo<T>(factory: () => T, deps: readonly unknown[]): T;
  export function useState<T>(initialState: T | (() => T)): [T, (value: T | ((prev: T) => T)) => void];
  export function useEffect(effect: () => void | (() => void), deps?: readonly unknown[]): void;
  export function useCallback<T extends (...args: unknown[]) => unknown>(callback: T, deps: readonly unknown[]): T;

  const React: {
    useMemo: typeof useMemo;
    useState: typeof useState;
    useEffect: typeof useEffect;
    useCallback: typeof useCallback;
  };
  export default React;
}

declare module '@openshift-console/dynamic-plugin-sdk' {
  import type { ComponentType } from 'react';

  export const ResourceLink: ComponentType<{
    groupVersionKind: { group: string; version: string; kind: string };
    name: string;
    namespace?: string;
  }>;
  export const ResourceIcon: ComponentType<{
    groupVersionKind: { group: string; version: string; kind: string };
  }>;
  export const Timestamp: ComponentType<{ timestamp?: string }>;
  export const useK8sModels: () => [Record<string, unknown>, boolean];
  export function useK8sWatchResources<T>(watches: Record<string, unknown>): T;
  export function useK8sWatchResource<T>(config: {
    groupVersionKind: { group: string; version: string; kind: string };
    isList: boolean;
    namespaced: boolean;
    namespace?: string;
  }): [T, boolean, Error | undefined];
}

declare module '@patternfly/react-core' {
  import type { ComponentType, ReactNode } from 'react';

  export const PageSection: ComponentType<{
    variant?: string;
    className?: string;
    children?: ReactNode;
  }>;
  export const Title: ComponentType<{
    headingLevel: 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6';
    children?: ReactNode;
  }>;
}

declare module 'react/jsx-runtime' {
  export const jsx: (type: unknown, props: Record<string, unknown>, key?: string) => unknown;
  export const jsxs: (type: unknown, props: Record<string, unknown>, key?: string) => unknown;
  export const Fragment: symbol;
}

// Ensure JSX types exist
declare namespace JSX {
  interface IntrinsicElements {
    [elemName: string]: Record<string, unknown>;
  }
}
