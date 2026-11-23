import { Call } from "@wailsio/runtime";

const SERVICE = "main.VMService";

const listEl = document.querySelector<HTMLUListElement>("#registries")!;
const addForm = document.querySelector<HTMLFormElement>("#add-registry")!;
const registryEl = document.querySelector<HTMLInputElement>("#registry")!;
const loginForm = document.querySelector<HTMLFormElement>("#login")!;
const loginServerEl = document.querySelector<HTMLElement>("#login-server")!;
const usernameEl = document.querySelector<HTMLInputElement>("#username")!;
const passwordEl = document.querySelector<HTMLInputElement>("#password")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#registry-message")!;

let selected = "";

function render(registries: string[]): void {
  listEl.replaceChildren(
    ...registries.map((server) => {
      const item = document.createElement("li");
      const name = document.createElement("span");
      name.textContent = server;
      const login = document.createElement("button");
      login.type = "button";
      login.textContent = "ログイン";
      login.addEventListener("click", () => {
        selected = server;
        loginServerEl.textContent = server;
        loginForm.hidden = false;
        messageEl.textContent = "";
        usernameEl.focus();
      });
      const remove = document.createElement("button");
      remove.type = "button";
      remove.textContent = "削除";
      remove.addEventListener("click", async () => {
        render((await Call.ByName(`${SERVICE}.RemoveRegistry`, server)) as string[]);
      });
      item.append(name, login, remove);
      return item;
    }),
  );
}

addForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  messageEl.textContent = "";
  try {
    render((await Call.ByName(`${SERVICE}.AddRegistry`, registryEl.value)) as string[]);
    registryEl.value = "";
  } catch (error) {
    messageEl.textContent = String(error);
  }
});

loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  messageEl.textContent = "ログインしています…";
  try {
    await Call.ByName(`${SERVICE}.Login`, selected, usernameEl.value, passwordEl.value);
    messageEl.textContent = `${selected} にログインしました`;
    loginForm.hidden = true;
  } catch (error) {
    messageEl.textContent = `ログインできませんでした: ${String(error)}`;
  } finally {
    // Never keep the password around in the page.
    passwordEl.value = "";
  }
});

export async function initRegistries(): Promise<void> {
  render(((await Call.ByName(`${SERVICE}.Registries`)) as string[] | null) ?? []);
}
