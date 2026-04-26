// pm2 ecosystem config example.
//
// One pm2 app per stack. Copy this file, customise the apps array, and
// run `pm2 start ecosystem.config.js`.
//
// compose-remote writes structured key=value logs to stdout/stderr; pm2
// captures and rotates them automatically.

module.exports = {
  apps: [
    {
      name: 'web-stack',
      script: 'compose-remote',
      args: [
        'run',
        '--name', 'web-stack',
        '--git', 'https://github.com/me/infra.git',
        '--git-ref', 'main',
        '--git-path', 'stacks/web/docker-compose.yml',
        '--interval', '30s',
      ],
      autorestart: true,
      max_memory_restart: '128M',
      env: {
        // Set if you want verbose docker-call logs.
        // COMPOSE_REMOTE_DEBUG: '1',
      },
    },

    {
      name: 'monitoring-stack',
      script: 'compose-remote',
      args: [
        'run',
        '--name', 'monitoring',
        '--url', 'https://config.example.com/monitoring/compose.yml',
      ],
      autorestart: true,
      max_memory_restart: '128M',
    },
  ],
};
