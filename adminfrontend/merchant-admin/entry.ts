import { applicationStorageKey, initializeApplication } from '../src/config/application';
import { restoreApplicationTheme } from '../src/config/appearance';

initializeApplication()
  .then(() => {
    restoreApplicationTheme(applicationStorageKey('preferences'));
    return import('../src/main');
  })
  .catch((error: unknown) => {
    console.error('Failed to initialize admin application:', error);
    const root = document.getElementById('root');
    if (root) root.textContent = '后台部署配置不可用，请联系管理员。';
  });
