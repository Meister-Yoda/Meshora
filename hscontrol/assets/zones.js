(() => {
  "use strict";
  const dialog = document.getElementById("zone-editor");
  if (!dialog) return;
  const opener = [...document.querySelectorAll("[data-zone-opener]")].find(
    (element) => element.dataset.zoneOpener === dialog.dataset.zoneOwner,
  );
  for (const button of dialog.querySelectorAll("[data-zone-close]")) {
    button.addEventListener("click", () => dialog.close());
  }
  dialog.addEventListener("click", (event) => {
    if (event.target !== dialog) return;
    const rect = dialog.getBoundingClientRect();
    if (
      event.clientX < rect.left ||
      event.clientX > rect.right ||
      event.clientY < rect.top ||
      event.clientY > rect.bottom
    )
      dialog.close();
  });
  dialog.addEventListener("close", () => {
    const url = new URL(window.location.href);
    url.searchParams.delete("new");
    url.searchParams.delete("zone");
    history.replaceState(null, "", url);
    opener?.focus({ preventScroll: true });
  });
  if (dialog.open && typeof dialog.showModal === "function") {
    dialog.removeAttribute("open");
    opener?.focus({ preventScroll: true });
    dialog.showModal();
    const target = dialog.querySelector("[aria-invalid='true'], [data-zone-error], #zone-name");
    target?.focus();
  }
})();
