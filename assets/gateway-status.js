/* Adapt sip connection events to the shared, transport-independent status surface. */
(function (global) {
    'use strict';
    const clientStatus = new WeakMap();
    function install(client) {
        const socket = new URL(client.urls.wsUrl, global.location.href);
        if (socket.host !== global.location.host || !socket.pathname.endsWith('/ws') || socket.search || socket.hash) {
            throw new Error('gateway status requires the same application WebSocket prefix');
        }
        const endpoint = socket.pathname.slice(0, -3) + '/_gateway/status';
        const status = global.FnPackKitConnectionStatus.create({
            endpoint,
            labels: global.fnGatewayStatusLabels,
            onRetry: () => client.reconnect(),
        });
        clientStatus.set(client, status);
        status.connecting();
    }
    function invoke(client, event) {
        const status = clientStatus.get(client);
        if (status) status[event]();
    }
    function transport(webterm, url) {
        const socket = new URL(url, global.location.href);
        if (socket.host !== global.location.host || !socket.pathname.endsWith('/ws') || socket.search || socket.hash) {
            throw new Error('gateway transport requires the same application WebSocket prefix');
        }
        const grantEndpoint = socket.pathname.slice(0, -3) + '/_gateway/grant';
        return global.FnPackKitGatewayTransport.create((address, options) => webterm.webSocketTransport(address, options), url, { grantEndpoint });
    }
    global.FnGatewayStatus = Object.freeze({
        install,
        transport,
        connecting: client => invoke(client, 'connecting'),
        connected: client => invoke(client, 'connected'),
        output: client => invoke(client, 'output'),
        failed: client => invoke(client, 'failed'),
    });
})(window);
