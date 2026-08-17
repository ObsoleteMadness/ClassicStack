/** ExtensionMapStore over ClassicStack’s /extmap HTTP API (Netatalk file on disk). */

import type { ExtensionMapStore, ExtensionMapping } from 'classicstack-web/fs/extension-map';
import { parseNetatalkExtensionMap, serializeNetatalkExtensionMap } from 'classicstack-web/fs/extension-map-netatalk';
import { api, type ConfigModel } from '../api';

const FALLBACK_PATH = 'extmap.conf';

function volumeExtMapPath(inst: Record<string, unknown>): string {
  const p = inst.ExtMapPath ?? inst.extmap_path;
  return typeof p === 'string' ? p.trim() : '';
}

export class HttpExtensionMapStore implements ExtensionMapStore {
  private path = '';
  private original = '';

  async resolvePath(model?: ConfigModel): Promise<string> {
    const cfg = model ?? (await api.config());
    for (const inst of cfg.Lists?.AFPVolumes ?? []) {
      const p = volumeExtMapPath(inst);
      if (p) return p;
    }
    return FALLBACK_PATH;
  }

  async load(): Promise<ExtensionMapping[]> {
    this.path = await this.resolvePath();
    const { content } = await api.extMap(this.path);
    this.original = content ?? '';
    return parseNetatalkExtensionMap(this.original);
  }

  async save(rows: readonly ExtensionMapping[]): Promise<ExtensionMapping[]> {
    if (!this.path) this.path = await this.resolvePath();
    const content = serializeNetatalkExtensionMap(rows, this.original);
    await api.saveExtMap(this.path, content);
    this.original = content;
    return parseNetatalkExtensionMap(content);
  }
}
