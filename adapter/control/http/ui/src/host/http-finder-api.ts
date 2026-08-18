import { ApiCatalog } from 'classicstack-web/finder/api-catalog';
import type { FinderAPI, ConnectRequest } from 'classicstack-web/finder/api';
import type { FinderNodeDto, FinderSessionDto, OpProgress, CrossTransferRequest } from 'classicstack-web/finder/types';
import { readSSEProgress } from 'classicstack-web/finder/progress';
import { api } from '../api';
import { bytesToB64 } from '../bytes';

export class HttpFinderAPI implements FinderAPI {
  readonly backendId = 'http';

  async getNode(sessionId: string, id: number): Promise<FinderNodeDto> {
    return api.finderNode(sessionId, id);
  }
  async children(sessionId: string, parentId: number): Promise<FinderNodeDto[]> {
    return api.finderChildren(sessionId, parentId);
  }
  async lookup(sessionId: string, parentId: number, name: string): Promise<FinderNodeDto | null> {
    return api.finderLookup(sessionId, parentId, name);
  }
  async mkdir(sessionId: string, parentId: number, name: string): Promise<FinderNodeDto> {
    return api.finderMkdir(sessionId, parentId, name);
  }
  async create(
    sessionId: string,
    parentId: number,
    name: string,
    body?: { data?: Uint8Array; resource?: Uint8Array; finderInfo?: Uint8Array },
  ): Promise<FinderNodeDto> {
    return api.finderCreate({
      sessionId,
      parentId,
      name,
      data: body?.data ? Array.from(body.data) : undefined,
      resource: body?.resource ? Array.from(body.resource) : undefined,
      finderInfo: body?.finderInfo ? bytesToB64(body.finderInfo) : undefined,
    });
  }
  async rename(sessionId: string, id: number, name: string): Promise<void> { return api.finderRename(sessionId, id, name); }
  async move(sessionId: string, id: number, parentId: number): Promise<void> { return api.finderMove(sessionId, id, parentId); }
  async remove(sessionId: string, id: number): Promise<void> { return api.finderRemove(sessionId, id); }

  async readFork(sessionId: string, id: number, resource: boolean, off?: number, len?: number): Promise<Uint8Array> {
    const q = new URLSearchParams({ session: sessionId, id: String(id), fork: resource ? 'resource' : 'data' });
    if (off != null) q.set('off', String(off));
    if (len != null) q.set('len', String(len));
    const r = await fetch(`finder/fork?${q.toString()}`);
    if (!r.ok) throw new Error(`fork read: HTTP ${r.status}`);
    return new Uint8Array(await r.arrayBuffer());
  }
  async writeFork(sessionId: string, id: number, resource: boolean, off: number, data: Uint8Array): Promise<void> {
    const q = new URLSearchParams({ session: sessionId, id: String(id), fork: resource ? 'resource' : 'data', off: String(off) });
    const r = await fetch(`finder/fork?${q.toString()}`, { method: 'PUT', body: data });
    if (!r.ok) throw new Error(`fork write: HTTP ${r.status}`);
  }
  async writeFinderInfo(sessionId: string, id: number, finderInfo: Uint8Array): Promise<void> {
    return api.finderFinderInfo(sessionId, id, bytesToB64(finderInfo));
  }

  copy(req: CrossTransferRequest, signal?: AbortSignal): AsyncIterable<OpProgress> {
    return this.readJob(api.finderCopy(req), signal);
  }
  moveAcross(req: CrossTransferRequest, signal?: AbortSignal): AsyncIterable<OpProgress> {
    return this.readJob(api.finderMoveAcross(req), signal);
  }
  expand(sessionId: string, id: number, signal?: AbortSignal): AsyncIterable<OpProgress> {
    return this.readJob(api.finderExpand({ sessionId, id }), signal);
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
