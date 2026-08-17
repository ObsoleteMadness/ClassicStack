/** Catalog over ClassicStack’s sessioned /finder HTTP API. */

import { CNIDRoot } from 'classicstack-web/protocol/afp/constants';
import type { Catalog, VNode, VfsChangeListener, ChildrenBatchListener } from 'classicstack-web/fs/virtual-fs';
import { importDataTransferInto, type ImportProgress } from 'classicstack-web/fs/import-transfer';
import { finderInfoFromName } from 'classicstack-web/fs/extension-map';
import { bufferRangeReader, type ByteRangeReader } from 'classicstack-web/fs/byte-range';
import { loadFinderIconFork, ResourceFork, type ResourceForkLoadOpts } from 'classicstack-web/fs/resource-fork';
import { iconForkLoadOptions } from 'classicstack-web/fs/icon-cache';
import { throwIfAborted, isAbortError } from 'classicstack-web/util/abort';
import { parseAppleDouble, parseAppleSingle, AS_MAGIC, AD_MAGIC } from 'classicstack-web/fs/appledouble';
import { be32 } from 'classicstack-web/protocol/binary';
import { unescapeHostFilename } from 'classicstack-web/protocol/host-filename';
import { api, type FinderNode } from '../api';
import { b64ToBytes, bytesToB64 } from '../bytes';

const EMPTY = new Uint8Array();

export class HttpCatalog implements Catalog {
  readonly reportsChunkedBytes = true;
  private sessionId: string;
  private root: number;
  private nodes = new Map<number, VNode>();
  private forksLoaded = new Set<number>();
  private listeners = new Set<VfsChangeListener>();
  private batchDepth = 0;
  private batchParents = new Set<number>();

  constructor(sessionId: string, rootId = CNIDRoot) {
    this.sessionId = sessionId;
    this.root = rootId || CNIDRoot;
  }

  rootId(): number {
    return this.root;
  }

  subscribe(fn: VfsChangeListener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  beginBatch(): void {
    this.batchDepth++;
  }

  endBatch(): void {
    if (this.batchDepth <= 0) return;
    this.batchDepth--;
    if (this.batchDepth === 0 && this.batchParents.size > 0) {
      const parentIds = [...this.batchParents];
      this.batchParents.clear();
      this.emit(parentIds);
    }
  }

  async get(id: number): Promise<VNode | undefined> {
    const cached = this.nodes.get(id);
    if (cached) return cached;
    try {
      return this.adopt(await api.finderNode(this.sessionId, id));
    } catch {
      return id === this.root ? this.ensureRoot() : undefined;
    }
  }

  async ensureContent(id: number, onBytes?: (n: number) => void, signal?: AbortSignal): Promise<VNode | undefined> {
    const node = await this.get(id);
    if (!node || node.isDir) return node;
    if (!this.forksLoaded.has(id)) await this.hydrateForks(node, onBytes, signal);
    return node;
  }

  async children(
    parentId: number,
    onBatch?: ChildrenBatchListener,
    signal?: AbortSignal,
  ): Promise<VNode[]> {
    throwIfAborted(signal);
    const raw = await api.finderChildren(this.sessionId, parentId);
    const kids = raw.map((n) => this.adopt(n));
    await onBatch?.(kids);
    return kids;
  }

  async lookup(parentId: number, name: string, signal?: AbortSignal): Promise<VNode | undefined> {
    throwIfAborted(signal);
    const lower = name.toLowerCase();
    for (const n of this.nodes.values()) {
      if (n.parentId === parentId && n.name.toLowerCase() === lower) return n;
    }
    const raw = await api.finderLookup(this.sessionId, parentId, name);
    return raw ? this.adopt(raw) : undefined;
  }

  async loadResourceFork(node: VNode, opts?: ResourceForkLoadOpts): Promise<ResourceFork | null> {
    throwIfAborted(opts?.signal);
    const resource = opts?.fork !== 'data';
    const loaded = resource ? node.resource.length : node.data.length;
    const hinted = resource ? (node.resourceBytes ?? loaded) : (node.dataBytes ?? loaded);
    if (Math.max(loaded, hinted) < 16) return null;
    try {
      const rangeOpts = { resource, signal: opts?.signal, priority: opts?.finderIcons ? 0 : 1 };
      const rf = await this.withRangeReader(
        node,
        (read) =>
          opts?.finderIcons
            ? loadFinderIconFork(read, iconForkLoadOptions(node))
            : ResourceFork.fromReader(read, opts?.want),
        rangeOpts,
      );
      rf?.bindFill((fn) => this.withRangeReader(node, fn, rangeOpts));
      return rf;
    } catch (err) {
      if (isAbortError(err)) throw err;
      return null;
    }
  }

  async loadIconResources(node: VNode, signal?: AbortSignal): Promise<ResourceFork | null> {
    return this.loadResourceFork(node, { finderIcons: true, signal });
  }

  async withRangeReader<T>(
    node: VNode,
    fn: (read: ByteRangeReader) => Promise<T>,
    opts?: { resource?: boolean; signal?: AbortSignal; priority?: number },
  ): Promise<T> {
    throwIfAborted(opts?.signal);
    if (this.forksLoaded.has(node.id)) {
      const bytes = opts?.resource ? node.resource : node.data;
      return fn(bufferRangeReader(bytes));
    }
    const fork = opts?.resource ? 'resource' : 'data';
    const read: ByteRangeReader = async (offset, count) => {
      throwIfAborted(opts?.signal);
      if (count <= 0) return new Uint8Array();
      const q = `session=${encodeURIComponent(this.sessionId)}&id=${node.id}&fork=${fork}&off=${offset}&len=${count}`;
      const r = await fetch(`finder/fork?${q}`, {
        headers: { Range: `bytes=${offset}-${offset + count - 1}` },
        signal: opts?.signal,
      });
      if (!r.ok) throw new Error(`fork read: HTTP ${r.status}`);
      return new Uint8Array(await r.arrayBuffer());
    };
    return fn(read);
  }

  async mkdir(parentId: number, name: string): Promise<VNode> {
    const existing = await this.lookup(parentId, name);
    if (existing) throw new Error('exists');
    const node = this.adopt(await api.finderMkdir(this.sessionId, parentId, name));
    this.notify(parentId);
    return node;
  }

  async ensureDir(parentId: number, name: string): Promise<VNode> {
    const existing = await this.lookup(parentId, name);
    if (existing?.isDir) return existing;
    if (existing) throw new Error('exists');
    return this.mkdir(parentId, name);
  }

  async createFile(
    parentId: number,
    name: string,
    data: Uint8Array,
    resource = new Uint8Array(),
    finderInfo = finderInfoFromName(name),
    onBytes?: (n: number) => void,
    signal?: AbortSignal,
  ): Promise<VNode> {
    throwIfAborted(signal);
    const raw = await api.finderCreate({
      sessionId: this.sessionId,
      parentId,
      name,
      finderInfo: bytesToB64(finderInfo),
    });
    const node = this.adopt(raw);
    if (data.length) await this.writeFork(node.id, false, data, onBytes, signal);
    throwIfAborted(signal);
    if (resource.length) await this.writeFork(node.id, true, resource, onBytes, signal);
    node.data = data;
    node.resource = resource;
    node.finderInfo = finderInfo;
    node.dataBytes = data.length;
    node.resourceBytes = resource.length;
    this.forksLoaded.add(node.id);
    this.nodes.set(node.id, node);
    this.notify(parentId);
    return node;
  }

  async put(node: VNode): Promise<void> {
    this.nodes.set(node.id, node);
    await api.finderFinderInfo(this.sessionId, node.id, bytesToB64(node.finderInfo));
    this.notify(node.parentId);
  }

  async rename(id: number, newName: string): Promise<void> {
    const n = this.nodes.get(id) ?? (await this.get(id));
    if (!n) throw new Error('not found');
    if (n.id === this.rootId()) throw new Error('cannot rename volume root');
    await api.finderRename(this.sessionId, id, newName);
    n.name = newName;
    this.nodes.set(id, n);
    this.notify(n.parentId);
  }

  async move(id: number, newParent: number): Promise<void> {
    const n = this.nodes.get(id) ?? (await this.get(id));
    if (!n) throw new Error('not found');
    if (n.id === this.rootId()) throw new Error('cannot move volume root');
    if (n.parentId === newParent) return;
    await api.finderMove(this.sessionId, id, newParent);
    const oldParent = n.parentId;
    n.parentId = newParent;
    this.nodes.set(id, n);
    this.notify(oldParent, newParent);
  }

  async remove(id: number): Promise<void> {
    const n = this.nodes.get(id) ?? (await this.get(id));
    if (!n) return;
    if (n.id === this.rootId()) throw new Error('cannot delete volume root');
    if (n.isDir) {
      const kids = await this.children(id);
      for (const k of kids) await this.remove(k.id);
    }
    await api.finderRemove(this.sessionId, id);
    this.nodes.delete(id);
    this.forksLoaded.delete(id);
    this.notify(n.parentId);
  }

  async importDataTransfer(parentId: number, dt: DataTransfer, opts?: ImportProgress): Promise<number> {
    return importDataTransferInto(
      this,
      parentId,
      dt,
      (p, file, onBytes, resource, signal) => this.importBlob(p, file, onBytes, resource, signal),
      opts,
    );
  }

  private async importBlob(
    parentId: number,
    file: File,
    onBytes?: (n: number) => void,
    resource?: Uint8Array,
    signal?: AbortSignal,
  ): Promise<unknown> {
    throwIfAborted(signal);
    const buf = new Uint8Array(await file.arrayBuffer());
    const name = unescapeHostFilename(file.name);
    if (name.startsWith('._') && name.length > 2) {
      const ad = parseAppleDouble(buf);
      if (ad) {
        const target = name.slice(2);
        const existing = await this.lookup(parentId, target);
        if (existing && !existing.isDir) {
          const data = this.forksLoaded.has(existing.id)
            ? existing.data
            : await this.readWholeFork(existing.id, false, onBytes, signal);
          return this.createFile(parentId, target, data, ad.resource, ad.finderInfo, onBytes, signal);
        }
        return this.createFile(parentId, target, new Uint8Array(), ad.resource, ad.finderInfo, onBytes, signal);
      }
    }
    if (buf.length >= 4 && be32(buf, 0) === AS_MAGIC) {
      const as = parseAppleSingle(buf);
      if (as) return this.createFile(parentId, name, as.data, as.resource, as.finderInfo, onBytes, signal);
    }
    if (buf.length >= 4 && be32(buf, 0) === AD_MAGIC) {
      const ad = parseAppleDouble(buf);
      if (ad) return this.createFile(parentId, name, new Uint8Array(), ad.resource, ad.finderInfo, onBytes, signal);
    }
    return this.createFile(parentId, name, buf, resource ?? EMPTY, undefined, onBytes, signal);
  }

  private async hydrateForks(node: VNode, onBytes?: (n: number) => void, signal?: AbortSignal): Promise<void> {
    throwIfAborted(signal);
    try {
      node.data = await this.readWholeFork(node.id, false, onBytes, signal);
    } catch (err) {
      if (isAbortError(err)) throw err;
      node.data = EMPTY;
    }
    try {
      node.resource = await this.readWholeFork(node.id, true, onBytes, signal);
    } catch (err) {
      if (isAbortError(err)) throw err;
      node.resource = EMPTY;
    }
    node.dataBytes = node.data.length;
    node.resourceBytes = node.resource.length;
    this.forksLoaded.add(node.id);
    this.nodes.set(node.id, node);
  }

  private async writeFork(
    id: number,
    resource: boolean,
    data: Uint8Array,
    onBytes?: (n: number) => void,
    signal?: AbortSignal,
  ): Promise<void> {
    const fork = resource ? 'resource' : 'data';
    const chunk = 256 * 1024;
    for (let off = 0; off < data.length; off += chunk) {
      throwIfAborted(signal);
      const slice = data.subarray(off, Math.min(data.length, off + chunk));
      const q = `session=${encodeURIComponent(this.sessionId)}&id=${id}&fork=${fork}&off=${off}`;
      const r = await fetch(`finder/fork?${q}`, { method: 'PUT', body: slice, signal });
      if (!r.ok) throw new Error(`fork write: HTTP ${r.status}`);
      onBytes?.(slice.length);
    }
  }

  private async readWholeFork(
    id: number,
    resource: boolean,
    onBytes?: (n: number) => void,
    signal?: AbortSignal,
  ): Promise<Uint8Array> {
    const fork = resource ? 'resource' : 'data';
    const q = `session=${encodeURIComponent(this.sessionId)}&id=${id}&fork=${fork}`;
    const r = await fetch(`finder/fork?${q}`, { signal });
    if (!r.ok) return EMPTY;
    const buf = new Uint8Array(await r.arrayBuffer());
    const chunk = 256 * 1024;
    if (onBytes) {
      for (let i = 0; i < buf.length; i += chunk) onBytes(Math.min(chunk, buf.length - i));
    }
    return buf;
  }

  private adopt(raw: FinderNode): VNode {
    const prev = this.nodes.get(raw.id);
    const loaded = this.forksLoaded.has(raw.id);
    const node: VNode = {
      id: raw.id,
      parentId: raw.parentId,
      name: raw.name,
      isDir: raw.isDir,
      data: loaded && prev ? prev.data : EMPTY,
      resource: loaded && prev ? prev.resource : EMPTY,
      finderInfo: b64ToBytes(raw.finderInfo).length ? b64ToBytes(raw.finderInfo) : new Uint8Array(32),
      createDate: raw.createDate ?? 0,
      modDate: raw.modDate ?? 0,
      dataBytes: raw.isDir ? 0 : (raw.dataBytes ?? 0),
      resourceBytes: raw.isDir ? 0 : (raw.resourceBytes ?? 0),
    };
    if (!raw.isDir && loaded && prev) {
      node.dataBytes = prev.data.length;
      node.resourceBytes = prev.resource.length;
    }
    this.nodes.set(node.id, node);
    return node;
  }

  private ensureRoot(): VNode {
    const existing = this.nodes.get(this.root);
    if (existing) return existing;
    const root: VNode = {
      id: this.root,
      parentId: 1,
      name: '',
      isDir: true,
      data: EMPTY,
      resource: EMPTY,
      finderInfo: new Uint8Array(32),
      createDate: 0,
      modDate: 0,
    };
    this.nodes.set(this.root, root);
    return root;
  }

  private notify(...parentIds: number[]): void {
    if (this.batchDepth > 0) {
      for (const id of parentIds) this.batchParents.add(id);
      return;
    }
    this.emit(parentIds);
  }

  private emit(parentIds: number[]): void {
    const change = { parentIds };
    for (const fn of this.listeners) fn(change);
  }
}
