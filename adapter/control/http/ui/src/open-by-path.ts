/** Advanced → Open by Path: connect with a csfs / csclient URI. */

import type { FinderWindow } from 'classicstack-web/ui/finder-window';
import type { RemoteEndpoint } from 'classicstack-web/ui/finder-host';
import { promptChoice, promptText } from './admin/prompt';
import {
  ClientURIError,
  credentialsFromTarget,
  endpointFromTarget,
  matchEndpoint,
  parseClientURI,
} from './fs/client-uri';
import type { GoFinderHost } from './host/go-finder-host';

const PATH_HINT =
  'Examples: afp://server:Zone/Volume  ·  smb://user@host,tcp/share  ·  ncp://SERVER,ipx/SYS';

export async function openByPath(finder: FinderWindow, host: GoFinderHost): Promise<void> {
  const raw = await promptText('Open by Path', 'Client URI (same as csfs / csclient)', '', {
    okLabel: 'Connect',
    multiline: false,
    placeholder: 'afp://server:Zone/Volume',
    hint: PATH_HINT,
  });
  if (!raw) return;

  let target;
  try {
    target = parseClientURI(raw);
  } catch (e) {
    const msg = e instanceof ClientURIError ? e.message : e instanceof Error ? e.message : String(e);
    host.showAlert('Invalid path', msg);
    return;
  }

  let list: RemoteEndpoint[] = [];
  try {
    list = await host.cachedNetwork();
    finder.setServers(list);
  } catch {
    /* connect anyway with a synthetic endpoint */
  }

  const ep = matchEndpoint(list, target) ?? endpointFromTarget(target);
  const creds = credentialsFromTarget(target);
  const folderPath = target.path || undefined;

  const first = await finder.openRemote(ep, {
    volume: target.volume || undefined,
    credentials: creds,
    autoOpenSingle: !!target.volume,
    folderPath: target.volume ? folderPath : undefined,
  });
  if (!first.ok) return;

  if (target.volume) return;

  const volumes = first.volumes;
  if (!volumes.length) {
    host.showAlert('No volumes', `Signed in to ${ep.title}, but the server advertised no volumes.`);
    return;
  }

  const picked = await promptChoice(
    'Select a Volume',
    `Connected to ${ep.title}. Choose a volume to open.`,
    volumes,
  );
  if (!picked) return;

  await finder.openRemote(ep, {
    volume: picked,
    autoOpenSingle: false,
    folderPath,
  });
}
