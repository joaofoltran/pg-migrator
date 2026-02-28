import { useEffect, useRef, useState, useCallback } from "react";
import type { Snapshot } from "../types/metrics";
import { WSConnection } from "../api/websocket";
import { fetchStatus } from "../api/client";

const MAX_HISTORY = 300;

export function useMetrics() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [connected, setConnected] = useState(false);
  const [history, setHistory] = useState<Snapshot[]>([]);
  const wsRef = useRef<WSConnection | null>(null);

  const onSnapshot = useCallback((snap: Snapshot) => {
    setSnapshot(snap);
    setHistory((prev) => {
      const next = [...prev, snap];
      if (next.length > MAX_HISTORY) {
        return next.slice(next.length - MAX_HISTORY);
      }
      return next;
    });
  }, []);

  const onStatus = useCallback((status: boolean) => {
    setConnected(status);
  }, []);

  useEffect(() => {
    fetchStatus()
      .then((snap) => {
        if (snap) setSnapshot(snap);
      })
      .catch(() => {});

    const ws = new WSConnection(onSnapshot, onStatus);
    wsRef.current = ws;
    return () => ws.close();
  }, [onSnapshot, onStatus]);

  return { snapshot, connected, history };
}
