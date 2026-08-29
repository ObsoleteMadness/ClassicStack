import {
  Folder,
  Monitor,
  Share2,
  Shield,
  Terminal,
  type IconNode,
} from 'lucide';
import { settingsBitmapIcons, settingsIconImg } from 'classicstack-web/ui/settings-icons';

function svg(icon: IconNode, size = 18): string {
  const inner = icon
    .map(([tag, attrs]) => {
      const a = Object.entries(attrs)
        .filter(([, v]) => v != null)
        .map(([k, v]) => `${k}="${String(v).replace(/"/g, '&quot;')}"`)
        .join(' ');
      return `<${tag} ${a}/>`;
    })
    .join('');
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${inner}</svg>`;
}

export const settingsIcons = {
  ...settingsBitmapIcons,
  users: settingsIconImg('/icons/classic/users.png'),
  macip: settingsIconImg('/icons/classic/apple-ip-gw.png'),
  ipxgw: settingsIconImg('/icons/classic/macipx-cp.png'),
  web: svg(Monitor),
  client: svg(Share2),
  fuse: svg(Folder),
  logging: svg(Terminal),
  advanced: svg(Shield),
};
