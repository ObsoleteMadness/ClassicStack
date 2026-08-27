/** Shared SSE bus for stats / state / log / message / finder topics. */

import type { FinderVolume, LogRecord } from './api';

const LOG_BUFFER_MAX = 2000;

export type StatsSample = { Component: string; Stats?: Record<string, unknown> };

export type ServerMessage = {
  Kind?: string;
  From?: string;
  To?: string;
  Text?: string;
  Time?: string;
};

export type FinderEvent = {
  Kind?: string;
  Scheme?: string;
  Scanning?: boolean;
  Volumes?: FinderVolume[];
  Time?: string;
};

export type LiveConn = 'connecting' | 'connected' | 'offline';

export function isServerOffline(): boolean {
  return telemetry.conn === 'offline';
}

export const telemetry = {
  source: null as EventSource | null,
  stats: {} as Record<string, Record<string, unknown>>,
  logs: [] as LogRecord[],
  conn: 'connecting' as LiveConn,
  onStats: new Set<(s: StatsSample) => void>(),
  onState: new Set<(s: unknown) => void>(),
  onLog: new Set<(r: LogRecord) => void>(),
  onMessage: new Set<(m: ServerMessage) => void>(),
  onFinder: new Set<(f: FinderEvent) => void>(),
  onConn: new Set<(s: LiveConn) => void>(),
  start() {
    if (this.source) return;
    const es = new EventSource('subscribe?topics=stats,state,log,message,finder');
    const setConn = (s: LiveConn): void => {
      if (this.conn === s) return;
      this.conn = s;
      this.onConn.forEach((cb) => cb(s));
    };
    es.onopen = () => setConn('connected');
    es.onerror = () => setConn('offline');
    es.addEventListener('stats', (e) => {
      try {
        const s = JSON.parse((e as MessageEvent).data) as StatsSample;
        this.stats[s.Component] = (s.Stats || {}) as Record<string, unknown>;
        this.onStats.forEach((cb) => cb(s));
      } catch {
        /* ignore malformed samples */
      }
    });
    es.addEventListener('state', (e) => {
      try {
        const s = JSON.parse((e as MessageEvent).data);
        this.onState.forEach((cb) => cb(s));
      } catch {
        /* ignore */
      }
    });
    es.addEventListener('log', (e) => {
      try {
        const rec = JSON.parse((e as MessageEvent).data) as LogRecord;
        this.logs.push(rec);
        if (this.logs.length > LOG_BUFFER_MAX) this.logs.splice(0, this.logs.length - LOG_BUFFER_MAX);
        this.onLog.forEach((cb) => cb(rec));
      } catch {
        /* ignore */
      }
    });
    es.addEventListener('message', (e) => {
      try {
        const rec = JSON.parse((e as MessageEvent).data) as ServerMessage;
        this.onMessage.forEach((cb) => cb(rec));
      } catch {
        /* ignore */
      }
    });
    es.addEventListener('finder', (e) => {
      try {
        const rec = JSON.parse((e as MessageEvent).data) as FinderEvent;
        this.onFinder.forEach((cb) => cb(rec));
      } catch {
        /* ignore */
      }
    });
    this.source = es;
  },
};
