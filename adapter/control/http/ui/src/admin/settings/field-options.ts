/** Known option sets for settings checklists and pickers. */

export const BACKEND_OPTIONS = ['pcap', 'tap', 'tun'] as const;

export const LOG_LEVELS = ['debug', 'info', 'warn', 'error'] as const;

export const IPX_FRAME_OPTIONS = [
  { value: 'ethernet_ii', label: 'Ethernet II (DIX)' },
  { value: '802.3', label: 'Ethernet 802.3 (raw Novell)' },
  { value: '802.2', label: 'Ethernet 802.2 (LLC)' },
] as const;

export const NETBIOS_TRANSPORT_OPTIONS = [
  { value: 'netbeui', label: 'NetBEUI (NBF)' },
  { value: 'ipx', label: 'IPX (NB-IPX)' },
  { value: 'nbt', label: 'NBT (TCP/IP)' },
] as const;

export const SMB_TRANSPORT_OPTIONS = [
  { value: 'netbeui', label: 'NetBEUI' },
  { value: 'ipx', label: 'IPX' },
  { value: 'nbt', label: 'NBT' },
  { value: 'tcp', label: 'Direct TCP (:445)' },
] as const;

export const AFP_TRANSPORT_OPTIONS = [
  { value: 'ddp', label: 'DDP (Classic ASP/ATP)' },
  { value: 'tcp', label: 'TCP (DSI)' },
] as const;

export const CLIENT_SERVICE_OPTIONS = [
  { value: 'afp', label: 'AFP' },
  { value: 'smb', label: 'SMB' },
  { value: 'ncp', label: 'NCP' },
  { value: 'etherdfs', label: 'EtherDFS' },
] as const;

export const MACIP_MODE_OPTIONS = [
  { value: 'bridge', label: 'Bridge (proxy-ARP)' },
  { value: 'nat', label: 'NAT' },
] as const;

/** Process-global Netatalk extension map (Settings → General → File type mappings). */
export const GLOBAL_EXTMAP_PATH = 'extmap.conf';

/** Fallback picker lists when GET /share_backends is unavailable. */
export const FALLBACK_FS_TYPES = ['local_fs', 'memfs', 'zipfs'];
export const FALLBACK_FORK_BACKENDS = [
  'appledouble',
  'appledouble-osxzip',
  'appledouble-dir',
  'nofork',
  'ads',
  'xattr',
  'native',
];
export const FALLBACK_FILENAME_CODECS = ['identity', 'windows-safe', 'macroman-utf8', 'macroman-native'];
export const FALLBACK_METASTORES = ['mem', 'sqlite'];
export const FALLBACK_META_BACKENDS = ['metastore', 'xattr', 'ads'];

export type CheckOption = { value: string; label: string };

/** Transport checklist options keyed by singleton schema key. */
export function transportOptions(schemaKey: string): CheckOption[] | null {
  switch (schemaKey) {
    case 'NetBIOS':
      return [...NETBIOS_TRANSPORT_OPTIONS];
    case 'SMB':
      return [...SMB_TRANSPORT_OPTIONS];
    case 'AFP':
      return [...AFP_TRANSPORT_OPTIONS];
    default:
      return null;
  }
}
