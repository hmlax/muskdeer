import type { Connect, Plugin } from 'vite';

/** Development/preview use the same runtime configuration endpoint as nginx. */
export function applicationConfigPlugin(merchantCode: string): Plugin {
  const middleware: Connect.NextHandleFunction = (request, response, next) => {
    if (request.url?.split('?')[0] !== '/app-config.json') return next();
    response.setHeader('Content-Type', 'application/json; charset=utf-8');
    response.setHeader('Cache-Control', 'no-store');
    response.end(JSON.stringify({ merchantCode }));
  };

  return {
    name: 'admin-application-config',
    configureServer(server) {
      server.middlewares.use(middleware);
    },
    configurePreviewServer(server) {
      server.middlewares.use(middleware);
    },
  };
}
