import type { Snapshot } from "../types/metrics";

type OnSnapshot = (snap: Snapshot) => void;
type OnStatus = (connected: boolean) => void;

export class WSConnection {
  private ws: WebSocket | null = null;
  private url: string;
  private onSnapshot: OnSnapshot;
  private onStatus: OnStatus;
  private reconnectDelay = 1000;
  private maxReconnectDelay = 30000;
  private closed = false;

  constructor(onSnapshot: OnSnapshot, onStatus: OnStatus) {
    const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
    this.url = `${proto}//${window.location.host}/api/v1/ws`;
    this.onSnapshot = onSnapshot;
    this.onStatus = onStatus;
    this.connect();
  }

  private connect() {
    if (this.closed) return;

    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      this.onStatus(true);
      this.reconnectDelay = 1000;
    };

    this.ws.onmessage = (event) => {
      try {
        const snap: Snapshot = JSON.parse(event.data);
        this.onSnapshot(snap);
      } catch {
        // Ignore parse errors.
      }
    };

    this.ws.onclose = () => {
      this.onStatus(false);
      if (!this.closed) {
        setTimeout(() => this.connect(), this.reconnectDelay);
        this.reconnectDelay = Math.min(
          this.reconnectDelay * 2,
          this.maxReconnectDelay
        );
      }
    };

    this.ws.onerror = () => {
      this.ws?.close();
    };
  }

  close() {
    this.closed = true;
    this.ws?.close();
  }
}
