// Inside the Playwright container: forward 127.0.0.1:8443 to the dev
// stack's Caddy, so https://*.localhost:8443 works as on the host.
const net = require('node:net');
net
  .createServer((c) => {
    const u = net.connect(443, process.env.E2E_CADDY_HOST || 'caddy');
    c.pipe(u);
    u.pipe(c);
    c.on('error', () => u.destroy());
    u.on('error', () => c.destroy());
  })
  .listen(8443, '127.0.0.1');
