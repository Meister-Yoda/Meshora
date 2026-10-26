(() => {
  "use strict";
  const mode = document.getElementById("link-mode");
  const services = document.querySelector("[data-link-services]");
  if (mode && services) {
    const update = () => {
      services.hidden = mode.value !== "services";
      for (const input of services.querySelectorAll("input")) {
        input.disabled = services.hidden;
        input.required = !services.hidden;
      }
    };
    mode.addEventListener("change", update);
    update();
  }
  for (const dialog of document.querySelectorAll("#zone-editor, #zone-pair-editor")) {
    const pair = dialog.id === "zone-pair-editor";
    const opener = pair
      ? document.querySelector('.zone-matrix-cell[aria-current="true"]')
      : [...document.querySelectorAll("[data-zone-opener]")].find(
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
      ) dialog.close();
    });
    dialog.addEventListener("close", () => {
      const url = new URL(window.location.href);
      for (const key of pair ? ["from", "to"] : ["new", "zone"]) {
        url.searchParams.delete(key);
      }
      if (pair) url.hash = "";
      history.replaceState(null, "", url);
      opener?.focus({ preventScroll: true });
    });
    if (dialog.open && typeof dialog.showModal === "function") {
      dialog.removeAttribute("open");
      opener?.focus({ preventScroll: true });
      dialog.showModal();
      const target = dialog.querySelector(
        pair
          ? "[data-zone-error], #link-mode, #zone-own-exits-mode"
          : "[aria-invalid='true'], [data-zone-error], #zone-name",
      );
      target?.focus();
    }
  }
})();
