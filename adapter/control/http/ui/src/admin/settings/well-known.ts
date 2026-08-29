import type { FieldInfo } from '../../api';

/** Static field metadata for Model fields outside GET /schemas. */
export const WELL_KNOWN: Record<string, FieldInfo[]> = {
  Identity: [
    { key: 'Hostname', type: 'string', display_name: 'Hostname', description: 'Server name for SMB, NetBIOS, and AFP.' },
    { key: 'Workgroup', type: 'string', display_name: 'Workgroup', description: 'SMB workgroup / browse domain.' },
    { key: 'Description', type: 'string', display_name: 'Description', description: 'Comment shown in browse lists.' },
  ],
  Router: [
    { key: 'DefaultZone', type: 'string', display_name: 'Default zone', description: 'Default AppleTalk zone name.' },
    {
      key: 'Members',
      type: 'strings',
      display_name: 'Router members',
      description: 'AppleTalk ports that join the router.',
      widget: 'port_members',
    },
  ],
  Logging: [
    {
      key: 'Level',
      type: 'string',
      display_name: 'Log level',
      description: 'Process log verbosity.',
      widget: 'select',
    },
    {
      key: 'Path',
      type: 'string',
      display_name: 'Log file path',
      description: 'Optional file the process logger appends to, in addition to stderr. Empty = stderr only. Takes effect after restart.',
      widget: 'path',
    },
  ],
  HTTP: [
    { key: 'Enabled', type: 'bool', display_name: 'Enabled', description: 'Serve the web-admin UI on this process.' },
    { key: 'Addr', type: 'string', display_name: 'Listen address', description: 'TCP host:port (empty = :1984).' },
  ],
  Client: [
    { key: 'Enabled', type: 'bool', display_name: 'Enabled', description: 'Run the in-process LAN file client.' },
    { key: 'Iface', type: 'string', display_name: 'Interface', description: 'Bridge name for outbound client traffic.', widget: 'iface' },
    {
      key: 'Services',
      type: 'strings',
      display_name: 'Services',
      description: 'File-sharing schemes the client probes.',
      widget: 'client_services',
    },
    { key: 'MaxIdleMinutes', type: 'int', display_name: 'Idle timeout (minutes)', description: 'Unused session idle time before disconnect.' },
    { key: 'Mount', type: 'bool', display_name: 'FUSE / WinFsp mounts', description: 'Allow host mounts of remote volumes.' },
    { key: 'LogFile', type: 'string', display_name: 'Client log file', description: 'Optional extra log path for client traffic.' },
  ],
  FUSE: [
    {
      key: 'MountTimeoutSeconds',
      type: 'int',
      display_name: 'Mount timeout (seconds)',
      description: 'How long to wait to connect to a remote server when mounting a volume.',
    },
  ],
};

/** Bridge interface fields (Kind omitted — always bridge). */
export const INTERFACE_FIELDS: FieldInfo[] = [
  { key: 'Name', type: 'string', display_name: 'Name', description: 'Namespace key ports reference (e.g. br-lan).' },
  { key: 'Backend', type: 'string', display_name: 'Backend', description: 'Link implementation for this bridge.', widget: 'backend' },
  { key: 'Default', type: 'bool', display_name: 'Default interface', description: 'Ports with no iface inherit this bridge.' },
  { key: 'Device', type: 'string', display_name: 'Host device', description: 'pcap/Npcap device name.', widget: 'host_device' },
  { key: 'HWAddress', type: 'string', display_name: 'Hardware address', description: 'Station MAC for injected frames (blank = NIC MAC).' },
  { key: 'Capture', type: 'string', display_name: 'Capture file', description: 'Optional pcap dump path for this bridge.' },
];
