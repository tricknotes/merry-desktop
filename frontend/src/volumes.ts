import { call } from "./native";

interface VolumeInfo {
  name: string;
  project: string;
}

interface VolumeBackup {
  file: string;
  sizeBytes: number;
  modified: string;
  volume: string;
}

interface BackupResult {
  file: string;
  sizeBytes: number;
  inUseBy: string[];
}

const folderEl = document.querySelector<HTMLElement>("#backup-folder")!;
const loadEl = document.querySelector<HTMLButtonElement>("#volumes-load")!;
const box = document.querySelector<HTMLDivElement>("#volumes-box")!;
const volumesEl = document.querySelector<HTMLUListElement>("#volumes")!;
const backupsEl = document.querySelector<HTMLUListElement>("#volume-backups")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#volumes-message")!;

function formatBytes(n: number): string {
  if (n >= 2 ** 30) return `${(n / 2 ** 30).toFixed(1)} GiB`;
  if (n >= 2 ** 20) return `${Math.round(n / 2 ** 20)} MiB`;
  return `${Math.round(n / 2 ** 10)} KiB`;
}

function empty(list: HTMLUListElement): void {
  const li = document.createElement("li");
  li.textContent = "ありません";
  list.replaceChildren(li);
}

/** Runs op with every button in the section disabled. */
async function busy(op: () => Promise<void>): Promise<void> {
  const buttons = [loadEl, ...box.querySelectorAll("button")];
  buttons.forEach((b) => (b.disabled = true));
  try {
    await op();
  } catch (error) {
    messageEl.textContent = String(error);
  } finally {
    buttons.forEach((b) => (b.disabled = false));
  }
}

function showVolumes(volumes: VolumeInfo[]): void {
  if (volumes.length === 0) return empty(volumesEl);
  volumesEl.replaceChildren(
    ...volumes.map((v) => {
      const li = document.createElement("li");
      const text = document.createElement("span");
      text.textContent = v.project ? `${v.name} ・ プロジェクト ${v.project}` : v.name;
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = "バックアップ";
      button.addEventListener("click", () =>
        busy(async () => {
          messageEl.textContent = `${v.name} をバックアップしています…`;
          const r = await call<BackupResult>("BackupVolume", v.name);
          let text = `${r.file}(${formatBytes(r.sizeBytes)})に保存しました。`;
          if (r.inUseBy.length > 0) {
            text += `動いているコンテナ(${r.inUseBy.join("、")})が使っていたため、書き込み中のデータは含まれていないかもしれません。`;
          }
          messageEl.textContent = text;
          showBackups(await call<VolumeBackup[]>("VolumeBackups"));
        }),
      );
      li.append(text, " ", button);
      return li;
    }),
  );
}

function showBackups(backups: VolumeBackup[]): void {
  if (backups.length === 0) return empty(backupsEl);
  backupsEl.replaceChildren(
    ...backups.map((b) => {
      const li = document.createElement("li");
      const text = document.createElement("span");
      text.textContent = `${b.file} ・ ${formatBytes(b.sizeBytes)} ・ ${new Date(b.modified).toLocaleString()}`;
      const form = document.createElement("form");
      form.className = "inline";
      const input = document.createElement("input");
      input.value = b.volume;
      input.required = true;
      input.ariaLabel = "復元先のボリューム";
      const button = document.createElement("button");
      button.type = "submit";
      button.textContent = "復元";
      form.append(input, button);
      form.addEventListener("submit", (e) => {
        e.preventDefault();
        const volume = input.value.trim();
        busy(async () => {
          messageEl.textContent = `${b.file} を ${volume} に復元しています…`;
          await call("RestoreVolume", b.file, volume);
          messageEl.textContent = `${volume} に復元しました。`;
          showVolumes(await call<VolumeInfo[]>("Volumes"));
        });
      });
      li.append(text, form);
      return li;
    }),
  );
}

loadEl.addEventListener("click", () =>
  busy(async () => {
    messageEl.textContent = "";
    const [volumes, backups] = await Promise.all([
      call<VolumeInfo[]>("Volumes"),
      call<VolumeBackup[]>("VolumeBackups"),
    ]);
    showVolumes(volumes);
    showBackups(backups);
    box.hidden = false;
  }),
);

export async function initVolumes(): Promise<void> {
  folderEl.textContent = await call<string>("BackupFolder");
}
