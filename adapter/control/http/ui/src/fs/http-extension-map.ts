/** ExtensionMapStore over ClassicStack’s /extmap HTTP API (Netatalk file on disk). */

import type { ExtensionMapStore, ExtensionMapping } from 'classicstack-web/fs/extension-map';
import { parseNetatalkExtensionMap, serializeNetatalkExtensionMap } from 'classicstack-web/fs/extension-map-netatalk';
import { GLOBAL_EXTMAP_PATH } from '../admin/settings/field-options';
import { api } from '../api';

export class HttpExtensionMapStore implements ExtensionMapStore {
  private path = '';
  private original = '';

  /** Settings → General → File type mappings always edits the process-global map. */
  async resolvePath(): Promise<string> {
    return GLOBAL_EXTMAP_PATH;
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
