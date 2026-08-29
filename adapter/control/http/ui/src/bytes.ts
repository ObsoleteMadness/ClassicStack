/** Encode bytes as a JSON `[]byte` (Go encoding/json base64 string). */
export function bytesToB64(u: Uint8Array): string {
  if (!u.length) return '';
  const chunk = 0x8000;
  let s = '';
  for (let i = 0; i < u.length; i += chunk) {
    s += String.fromCharCode(...u.subarray(i, i + chunk));
  }
  return btoa(s);
}

/** Decode a JSON `[]byte` (base64 string) to Uint8Array. */
export function b64ToBytes(s: string | null | undefined): Uint8Array {
  if (!s) return new Uint8Array();
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
