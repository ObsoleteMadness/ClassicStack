import {
  Folder,
  Globe,
  Monitor,
  Share2,
  Shield,
  Terminal,
  Users,
  type IconNode,
} from 'lucide';
import { settingsBitmapIcons } from 'classicstack-web/ui/settings-icons';

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
  users: svg(Users),
  macip: svg(Globe),
  ipxgw: svg(Globe),
  web: svg(Monitor),
  client: svg(Share2),
  fuse: svg(Folder),
  logging: svg(Terminal),
  advanced: svg(Shield),
};
