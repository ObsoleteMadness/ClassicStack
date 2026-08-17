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
import { api } from './api';
import { telemetry } from './telemetry';
import { renderSetup } from './admin/setup';
import { renderStatus } from './admin/status';
import { renderSharing } from './admin/sharing';
import { renderUsers } from './admin/users';
import { renderLogs } from './admin/logs';

type View = 'finder' | 'status' | 'sharing' | 'users' | 'logs';

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

  setConn('connected', 'ok');
  telemetry.start();
  setExtensionMapStore(new HttpExtensionMapStore());
  void hydrateExtensionMap().catch(() => undefined);

  const header = document.createElement('header');
  header.className = 'cs-shell';
  header.innerHTML = `
    <h1>ClassicStack</h1>
    <nav>
      <button type="button" class="tab active" data-view="finder">Finder</button>
      <button type="button" class="tab" data-view="status">Status</button>
      <button type="button" class="tab" data-view="sharing">Sharing</button>
      <button type="button" class="tab" data-view="users">Users</button>
      <button type="button" class="tab" data-view="logs">Logs</button>
    </nav>
    <span id="conn" class="badge ok">connected</span>
  `;

  const stage = document.createElement('div');
  stage.className = 'app-stage';
  const admin = document.createElement('div');
  admin.className = 'admin-stage';
  admin.hidden = true;

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

  stage.append(finder);
  app.replaceChildren(
    header,
    stage,
    admin,
    alertDialog,
    loginDialog,
    nameConflictDialog,
    resourceExplorer,
    getInfoWindow,
    extensionEditor,
  );

  const host = new GoFinderHost(loginDialog, alertDialog, nameConflictDialog);
  finder.bind(null, host);
  finder.bindResourceExplorer(resourceExplorer);
  finder.bindGetInfoWindow(getInfoWindow);
  mountFinderMenu(header, finder, extensionEditor);

  header.querySelector('nav')?.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-view]');
    if (!btn) return;
    show(btn.dataset.view as View);
  });

  function show(next: View) {
    header.querySelectorAll('.tab').forEach((b) => {
      b.classList.toggle('active', (b as HTMLElement).dataset.view === next);
    });
    const finderOn = next === 'finder';
    stage.hidden = !finderOn;
    admin.hidden = finderOn;
    if (finderOn) return;
    if (next === 'status') renderStatus(admin);
    else if (next === 'sharing') void renderSharing(admin);
    else if (next === 'users') void renderUsers(admin);
    else renderLogs(admin);
  }
}

void main();
