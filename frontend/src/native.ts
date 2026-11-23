// Calls into the Go side (internal/appservice.Service) and its events.
// In the app this goes through Wails bindings. In the development UI
// (`merry-dev ui`, internal/devui) it goes over HTTP; that page is opened with
// ?token=..., which is kept for the session so that links between pages work.

const SERVICE = "github.com/tricknotes/merry-desktop/internal/appservice.Service";
const TOKEN_KEY = "merry-dev-token";

function devToken(): string | null {
  const fromURL = new URLSearchParams(location.search).get("token");
  try {
    if (fromURL) sessionStorage.setItem(TOKEN_KEY, fromURL);
    return fromURL ?? sessionStorage.getItem(TOKEN_KEY);
  } catch {
    return fromURL;
  }
}

const token = devToken();

/** True in the development UI served by `merry-dev ui`. */
export const isDevUI = token !== null;

type Listener = (data: unknown) => void;
const listeners = new Map<string, Listener[]>();
let events: EventSource | null = null;

export async function call<T = unknown>(method: string, ...args: unknown[]): Promise<T> {
  if (!isDevUI) {
    const { Call } = await import("@wailsio/runtime");
    return (await Call.ByName(`${SERVICE}.${method}`, ...args)) as T;
  }
  const res = await fetch(`/api/call/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Merry-Token": token! },
    body: JSON.stringify(args),
  });
  if (!res.ok) throw new Error((await res.text()).trim());
  return (await res.json()) as T;
}

export function on(name: string, listener: Listener): void {
  if (!isDevUI) {
    void import("@wailsio/runtime").then(({ Events }) => Events.On(name, (e) => listener(e.data)));
    return;
  }
  listeners.set(name, [...(listeners.get(name) ?? []), listener]);
  if (!events) {
    events = new EventSource(`/api/events?token=${encodeURIComponent(token!)}`);
    events.onmessage = (message) => {
      const { name: event, data } = JSON.parse(message.data) as { name: string; data: unknown };
      for (const l of listeners.get(event) ?? []) l(data);
    };
  }
}

/** Shows links between the windows in the development UI (the app has the tray). */
export function showDevNav(): void {
  if (!isDevUI) return;
  const nav = document.createElement("nav");
  nav.className = "dev-nav";
  nav.innerHTML = '開発用 UI: <a href="/">設定</a> | <a href="/setup.html">セットアップ</a>';
  document.body.prepend(nav);
}

/** Shows notices from the VM (for example a slow bind mount) at the top of the page. */
export function showNotices(): void {
  on("vm:notice", (data) => {
    const box = document.createElement("p");
    box.className = "notice";
    box.textContent = String(data);
    box.addEventListener("click", () => box.remove());
    box.title = "クリックで閉じる";
    document.querySelector("main")?.prepend(box);
  });
}
