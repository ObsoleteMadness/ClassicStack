import 'classicstack-web/ui/styles/tokens.css';
import './admin.css';

import { startLayoutMode } from 'classicstack-web/ui/layout-mode';
import { FinderWindow } from 'classicstack-web/ui/finder-window';
import { AlertDialog } from 'classicstack-web/ui/alert-dialog';
import { LoginDialog } from 'classicstack-web/ui/login-dialog';
import { NameConflictDialog } from 'classicstack-web/ui/name-conflict-dialog';
import { ResourceForkExplorer } from 'classicstack-web/ui/resource-fork-explorer';
import { GetInfoWindow } from 'classicstack-web/ui/get-info-window';
import { ExtensionEditorDialog } from 'classicstack-web/ui/extension-editor-dialog';
import { hydrateExtensionMap, setExtensionMapStore } from 'classicstack-web/fs/extension-map';
import { GoFinderHost } from './host/go-finder-host';
import { HttpExtensionMapStore } from './fs/http-extension-map';
import { mountFinderMenu } from './finder-menu';
import { mountAppMenu } from './admin/app-menu';
import { ServerAboutDialog } from './admin/about-dialog';
import { ServerSettingsWindow } from './admin/server-settings-window';
import { LogWindow } from './admin/log-window';
import { NotificationCentre } from './admin/notifications';
import { SharingMonitorWindow } from './admin/sharing-monitor';
import { MacIPLeasesWindow } from './admin/macip-leases';
import { EndpointInfoWindow } from './admin/endpoint-info';
import { api } from './api';
import { telemetry, type ServerMessage } from './telemetry';
import { renderSetup } from './admin/setup';
import { mountControlPlane } from './admin/status';
import { TopologyWindow } from './admin/topology';

function setConn(text: string, cls = ''): void {
  const node = document.getElementById('conn');
  if (!node) return;
  node.textContent = text;
  node.className = 'badge' + (cls ? ' ' + cls : '');
}

async function main(): Promise<void> {
  startLayoutMode();
  const app = document.querySelector('#app')!;
  const { code } = await api.statusProbe();
  if (code === 409) {
    setConn('setup');
    renderSetup(app as HTMLElement);
    return;
  }
  if (code === 401) {
    setConn('locked', 'bad');
    app.innerHTML = `
      <div class="panel">
        <h2>Authentication required</h2>
        <p class="muted">Reload and enter the web-admin credentials.</p>
        <button type="button" class="btn primary" id="reload">Reload</button>
      </div>`;
    app.querySelector('#reload')?.addEventListener('click', () => location.reload());
    return;
  }
  if (code !== 200) {
    setConn('offline', 'bad');
    app.innerHTML = `<div class="panel err">Cannot reach the server (HTTP ${code}).</div>`;
    return;
  }

  telemetry.start();
  setExtensionMapStore(new HttpExtensionMapStore());
  void hydrateExtensionMap().catch(() => undefined);

  const header = document.createElement('header');
  header.className = 'cs-shell';
  header.innerHTML = `
    <h1>ClassicStack</h1>
    <div class="header-spacer"></div>
    <button type="button" class="notify-bell" id="notify-bell" aria-label="Notifications">
      <span class="notify-bell__glyph" aria-hidden="true">●</span>
      <span class="notify-bell__count"></span>
    </button>
  `;

  const workspace = document.createElement('div');
  workspace.className = 'workspace';
  const mainCol = document.createElement('div');
  mainCol.className = 'workspace-main';
  const finderScreen = document.createElement('div');
  finderScreen.className = 'finder-screen';
  const stage = document.createElement('div');
  stage.className = 'app-stage';

  const finder = new FinderWindow();
  finder.classList.add('is-maximized');
  const alertDialog = new AlertDialog();
  const loginDialog = new LoginDialog();
  const nameConflictDialog = new NameConflictDialog();
  const resourceExplorer = new ResourceForkExplorer();
  resourceExplorer.hidden = true;
  const getInfoWindow = new GetInfoWindow();
  getInfoWindow.hidden = true;
  const extensionEditor = new ExtensionEditorDialog();
  extensionEditor.hidden = true;
  const settings = new ServerSettingsWindow();
  const about = new ServerAboutDialog();
  const logWindow = new LogWindow();
  const sharing = new SharingMonitorWindow();
  const leases = new MacIPLeasesWindow();
  const notify = new NotificationCentre();
  const endpointInfo = new EndpointInfoWindow();
  const topology = new TopologyWindow();

  stage.append(finder);
  finderScreen.append(
    stage,
    loginDialog,
    nameConflictDialog,
    resourceExplorer,
    getInfoWindow,
    extensionEditor,
    settings,
    about,
    logWindow,
    sharing,
    leases,
    notify,
    endpointInfo,
    topology,
  );
  mainCol.append(finderScreen);
  workspace.append(mainCol);
  app.replaceChildren(header, workspace, alertDialog);

  const host = new GoFinderHost(loginDialog, alertDialog, nameConflictDialog, {
    onConfigureShare(ep) {
      settings.open(
        (ep.protocol?.toLowerCase() === 'smb' ? 'smb' : ep.protocol?.toLowerCase() === 'ncp' ? 'ncp' : ep.protocol?.toLowerCase() === 'etherdfs' ? 'etherdfs' : 'afp') as 'afp' | 'smb' | 'ncp' | 'etherdfs',
      );
    },
    onEndpointInfo(model) {
      endpointInfo.open(model);
    },
  });
  host.watchNetworks(() => {
    void host.cachedNetwork().then((list) => finder.setServers(list));
  });
  finder.bind(null, host);
  finder.bindResourceExplorer(resourceExplorer);
  finder.bindGetInfoWindow(getInfoWindow);
  mountAppMenu(header, { settings, about, log: logWindow, sharing, leases, notify, topology });
  mountFinderMenu(header, finder, extensionEditor);
  const bell = header.querySelector<HTMLButtonElement>('#notify-bell');
  if (bell) notify.bindBell(bell);
  mountControlPlane(header, workspace, notify);
  settings.bind({ finder, extensionEditor, leases });
  topology.openSharing = (protocol) => {
    settings.open(
      (protocol?.toLowerCase() === 'smb' ? 'smb' : protocol?.toLowerCase() === 'ncp' ? 'ncp' : protocol?.toLowerCase() === 'etherdfs' ? 'etherdfs' : 'afp') as 'afp' | 'smb' | 'ncp' | 'etherdfs',
    );
  };
  bindServerPopups(alertDialog);
}

function popupTitle(m: ServerMessage): { title: string; text: string } {
  const text = (m.Text || '').trim();
  if (m.Kind === 'messenger') {
    const from = (m.From || 'Messenger').trim() || 'Messenger';
    const to = (m.To || '').trim();
    const title = to ? `Message from ${from} to ${to}` : `Message from ${from}`;
    return { title, text };
  }
  return { title: (m.From || 'Server Message').trim() || 'Server Message', text };
}

function bindServerPopups(alert: AlertDialog): void {
  telemetry.onMessage.add((m) => {
    const { title, text } = popupTitle(m);
    if (!text) return;
    alert.show(title, text);
  });
}

void main();
