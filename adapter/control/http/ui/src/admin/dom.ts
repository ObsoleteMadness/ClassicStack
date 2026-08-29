/** DOM helpers for admin views. */

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string> = {},
  kids: (Node | string)[] = [],
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') node.className = v;
    else node.setAttribute(k, v);
  }
  for (const c of kids) node.append(c);
  return node;
}

export function btn(label: string, cls: string, onClick: () => void, disabled = false): HTMLButtonElement {
  const b = el('button', { type: 'button', class: 'btn' + (cls ? ' ' + cls : '') }, [label]);
  b.disabled = disabled;
  b.addEventListener('click', onClick);
  return b;
}

export function formatBytes(bytes: number): string {
  if (!bytes) return 'N/A';
  const k = 1024;
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  if (i < 0 || i >= sizes.length) return `${bytes} Bytes`;
  return `${(bytes / k ** i).toFixed(2)} ${sizes[i]}`;
}
