"use strict";
const copyButton = document.getElementById("enrollment-copy");
if (copyButton) {
  copyButton.disabled = false;
  copyButton.addEventListener("click", async () => {
    const value = document.getElementById("enrollment-value");
    const status = document.getElementById("enrollment-copy-status");
    try {
      await navigator.clipboard.writeText(value.value);
      status.textContent = document.getElementById("enrollment-copied").textContent;
    } catch {
      value.focus();
      value.select();
      status.textContent = document.getElementById("enrollment-copy-failed").textContent;
    }
  });
}
