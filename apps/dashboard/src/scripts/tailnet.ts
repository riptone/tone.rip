/**
 * Detects whether the visitor's browser is on the Tailscale network, by
 * looking for a Tailscale-range IP among the local ICE candidates gathered
 * for a throwaway WebRTC connection.
 */

/** Pure IP-range check: Tailscale's CGNAT range (100.64.0.0/10) or its IPv6 ULA prefix (fd7a:115c:a1e0::/48). */
export function isTailnetAddress(ip: string | undefined): boolean {
  if (!ip) return false;
  if (ip.startsWith("fd7a:115c:a1e0")) return true;
  const match = ip.match(/^(\d{1,3})\.(\d{1,3})\./);
  if (!match) return false;
  const secondOctet = Number(match[2]);
  return Number(match[1]) === 100 && secondOctet >= 64 && secondOctet <= 127;
}

interface TailnetDetectionOptions {
  timeoutMs: number;
}

/**
 * Impure by nature (needs a real WebRTC stack), so kept as a thin wrapper -
 * all the actual branching logic lives in isTailnetAddress, which is unit
 * tested. Not testable outside a browser; verify manually by joining/leaving
 * the tailnet and watching self-hosted tiles switch between `down` and `vpn`.
 */
export function detectTailnetPresence({
  timeoutMs,
}: TailnetDetectionOptions): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    try {
      const pc = new RTCPeerConnection({ iceServers: [] });
      const done = (value: boolean) => {
        try {
          pc.onicecandidate = null;
          pc.close();
        } catch {
          // no-op
        }
        resolve(value);
      };
      const timer = setTimeout(() => done(false), timeoutMs);

      pc.createDataChannel("x");
      pc.onicecandidate = (event) => {
        const candidate = event.candidate?.candidate;
        if (!candidate) return;
        const ip4 = candidate.match(/(\d{1,3}(?:\.\d{1,3}){3})/)?.[1];
        const ip6 = candidate.match(/([0-9a-fA-F:]{2,})/)?.[1];
        if (isTailnetAddress(ip4) || isTailnetAddress(ip6)) {
          clearTimeout(timer);
          done(true);
        }
      };
      pc.createOffer()
        .then((offer) => pc.setLocalDescription(offer))
        .catch(() => {
          clearTimeout(timer);
          done(false);
        });
    } catch {
      resolve(false);
    }
  });
}
