import { fileURLToPath } from 'node:url';
import { createAdminConfig } from '../build/vite/admin-config';

export default createAdminConfig(fileURLToPath(new URL('.', import.meta.url)), 'merchant');
