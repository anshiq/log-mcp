import { SystemService } from '../api';

class Connection {
  reachable = $state(true);
  version = $state('');
  apiVersion = $state('');
  uptimeSeconds = $state(0);
  socketPath = $state('');
  dataDir = $state('');
  tcpAddr = $state('');
  streams = $state<Record<string, string>>({});
  incompatible = $state(false);
  lastCheck = $state(0);

  async check() {
    try {
      const v = await SystemService.version();
      this.version = v.daemonVersion;
      this.apiVersion = v.apiVersion;
      this.reachable = true;
      this.lastCheck = Date.now();
      const h = await SystemService.health();
      this.uptimeSeconds = h.uptimeSeconds ?? Math.round((h.uptimeMs ?? 0) / 1000);
      this.socketPath = h.socketPath ?? '';
      this.dataDir = h.dataDir ?? '';
      this.tcpAddr = h.tcpAddr ?? '';
    } catch {
      this.reachable = false;
    }
  }

  setStream(name: string, state: string) {
    this.streams[name] = state;
  }

  get degraded(): boolean {
    return Object.values(this.streams).some((s) => s === 'reconnecting' || s === 'connecting');
  }

  start() {
    void this.check();
    const t = setInterval(() => {
      if (this.degraded || !this.reachable) void this.check();
    }, 5000);
    return () => clearInterval(t);
  }
}

export const connection = new Connection();
