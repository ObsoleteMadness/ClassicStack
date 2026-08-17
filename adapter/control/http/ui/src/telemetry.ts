/** Shared SSE bus for stats / state / log topics. */

import type { LogRecord } from './api';

const LOG_BUFFER_MAX = 2000;

export type StatsSample = { Component: string; Stats?: Record<string, unknown> };

export const telemetry = {
  source: null as EventSource | null,
  stats: {} as Record<string, Record<string, unknown>>,
  logs: [] as LogRecord[],
  onStats: new Set<(s: StatsSample) => void>(),
  onState: new Set<(s: unknown) => void>(),
  onLog: new Set<(r: LogRecord) => void>(),
  start() {
    if (this.source) return;
    const es = new EventSource('subscribe?topics=stats,state,log');
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
    this.source = es;
  },
};
