import { ApiCatalog } from 'classicstack-web/finder/api-catalog';
import type { FinderAPI, ConnectRequest } from 'classicstack-web/finder/api';
import type { FinderNodeDto, FinderSessionDto, OpProgress, CrossTransferRequest } from 'classicstack-web/finder/types';
import type { NodeRef } from 'classicstack-web/finder';
import { parentBody, refBody, refQuery } from 'classicstack-web/fs/catalog-caps';
import { readSSEProgress } from 'classicstack-web/finder/progress';
import { api } from '../api';
import { bytesToB64 } from '../bytes';

function forkQs(sessionId: string, ref: NodeRef, extra: Record<string, string>): URLSearchParams {
  const q = new URLSearchParams({ session: sessionId, ...refQuery(ref), ...extra });
  return q;
}

export class HttpFinderAPI implements FinderAPI {
  readonly backendId = 'http';

  async getNode(sessionId: string, ref: NodeRef): Promise<FinderNodeDto> {
    return api.finderNode(sessionId, ref);
  }
  async children(sessionId: string, parent: NodeRef): Promise<FinderNodeDto[]> {
    return api.finderChildren(sessionId, parent);
  }
  async lookup(sessionId: string, parent: NodeRef, name: string): Promise<FinderNodeDto | null> {
    return api.finderLookup(sessionId, parent, name);
  }
  async mkdir(sessionId: string, parent: NodeRef, name: string): Promise<FinderNodeDto> {
    return api.finderMkdir(sessionId, parent, name);
  }
  async create(
    sessionId: string,
    parent: NodeRef,
    name: string,
    body?: { data?: Uint8Array; resource?: Uint8Array; finderInfo?: Uint8Array },
  ): Promise<FinderNodeDto> {
    return api.finderCreate({
      sessionId,
      name,
      ...parentBody(parent),
      data: body?.data ? Array.from(body.data) : undefined,
      resource: body?.resource ? Array.from(body.resource) : undefined,
      finderInfo: body?.finderInfo ? bytesToB64(body.finderInfo) : undefined,
    });
  }
  async rename(sessionId: string, ref: NodeRef, name: string): Promise<void> {
    return api.finderRename(sessionId, ref, name);
  }
  async move(sessionId: string, ref: NodeRef, parent: NodeRef): Promise<void> {
    return api.finderMove(sessionId, ref, parent);
  }
  async remove(sessionId: string, ref: NodeRef): Promise<void> {
    return api.finderRemove(sessionId, ref);
  }

  async readFork(sessionId: string, ref: NodeRef, resource: boolean, off?: number, len?: number): Promise<Uint8Array> {
    const q = forkQs(sessionId, ref, { fork: resource ? 'resource' : 'data' });
    if (off != null) q.set('off', String(off));
    if (len != null) q.set('len', String(len));
    const r = await fetch(`finder/fork?${q.toString()}`);
    if (!r.ok) throw new Error(`fork read: HTTP ${r.status}`);
    return new Uint8Array(await r.arrayBuffer());
  }
  async writeFork(sessionId: string, ref: NodeRef, resource: boolean, off: number, data: Uint8Array): Promise<void> {
    const q = forkQs(sessionId, ref, { fork: resource ? 'resource' : 'data', off: String(off) });
    const r = await fetch(`finder/fork?${q.toString()}`, { method: 'PUT', body: data });
    if (!r.ok) throw new Error(`fork write: HTTP ${r.status}`);
  }
  async writeFinderInfo(sessionId: string, ref: NodeRef, finderInfo: Uint8Array): Promise<void> {
    return api.finderFinderInfo(sessionId, ref, bytesToB64(finderInfo));
  }
  async writeAttrs(sessionId: string, ref: NodeRef, patch: Record<string, boolean>): Promise<void> {
    return api.finderAttrs(sessionId, ref, patch);
  }
  async resolvePath(sessionId: string, path: string): Promise<FinderNodeDto | null> {
    try {
      return await api.finderResolve(sessionId, path);
    } catch {
      return null;
    }
  }
  async pathOf(sessionId: string, ref: NodeRef): Promise<string> {
    const { path } = await api.finderPathOf(sessionId, ref);
    return path;
  }

  copy(req: CrossTransferRequest, signal?: AbortSignal): AsyncIterable<OpProgress> {
    return this.readJob(api.finderCopy(req), signal);
  }
  moveAcross(req: CrossTransferRequest, signal?: AbortSignal): AsyncIterable<OpProgress> {
    return this.readJob(api.finderMoveAcross(req), signal);
  }
  expand(sessionId: string, ref: NodeRef, signal?: AbortSignal): AsyncIterable<OpProgress> {
    return this.readJob(api.finderExpand({ sessionId, ...refBody(ref) }), signal);
  }

  openCatalog(session: FinderSessionDto) {
    return new ApiCatalog(this, session);
  }
  connect(req: ConnectRequest): Promise<FinderSessionDto> {
    return api.finderConnect(req as Record<string, unknown>);
  }
  openVolume(sessionId: string, volume: string): Promise<FinderSessionDto> {
    return api.finderOpen(sessionId, volume);
  }
  close(sessionId: string): Promise<void> {
    return api.finderClose(sessionId);
  }
  closeVolume(sessionId: string, volume: string): Promise<void> {
    return api.finderCloseVolume(sessionId, volume);
  }

  private async *readJob(resp: Promise<Response>, signal?: AbortSignal): AsyncIterable<OpProgress> {
    if (signal?.aborted) throw signal.reason ?? new DOMException('Aborted', 'AbortError');
    const r = await resp;
    for await (const p of readSSEProgress(r)) {
      if (signal?.aborted) throw signal.reason ?? new DOMException('Aborted', 'AbortError');
      yield p;
    }
  }
}
