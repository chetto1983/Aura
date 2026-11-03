import net from 'node:net';

const socket = net.connect(3000, '127.0.0.1');
socket.on('connect', () => {
  process.stdin.pipe(socket);
  socket.pipe(process.stdout);
});
socket.on('error', () => process.exit(1));
socket.on('close', () => process.exit(0));
process.stdin.on('error', () => socket.destroy());
process.stdout.on('error', () => socket.destroy());
