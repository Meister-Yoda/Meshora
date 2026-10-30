"use strict";
(() => {
 const status = document.getElementById("passkey-status");
 const supported = window.isSecureContext && !!window.PublicKeyCredential && !!navigator.credentials;
 const decode = value => Uint8Array.from(atob(value.replace(/-/g, "+").replace(/_/g, "/")), c => c.charCodeAt(0));
 const encode = value => btoa(String.fromCharCode(...new Uint8Array(value))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
 const pack = credential => {
  if (typeof credential.toJSON === "function") return credential.toJSON();
  const response = {clientDataJSON: encode(credential.response.clientDataJSON)};
  for (const name of ["attestationObject", "authenticatorData", "signature", "userHandle"]) {
   if (credential.response[name]) response[name] = encode(credential.response[name]);
  }
  if (credential.response.getTransports) response.transports = credential.response.getTransports();
  return {id: credential.id, rawId: encode(credential.rawId), type: credential.type, response, clientExtensionResults: credential.getClientExtensionResults(), authenticatorAttachment: credential.authenticatorAttachment};
 };
 const options = (payload, register) => {
  const key = payload.publicKey;
  key.challenge = decode(key.challenge);
  if (register) key.user.id = decode(key.user.id);
  for (const list of [key.excludeCredentials, key.allowCredentials]) {
   if (list) for (const item of list) item.id = decode(item.id);
  }
  return {publicKey: key};
 };
 const request = async (url, csrf, body, json) => {
  const response = await fetch(url, {method: "POST", credentials: "same-origin", headers: {"X-CSRF-Token": csrf, "Accept": "application/json", ...(json ? {"Content-Type": "application/json"} : {})}, body: json ? JSON.stringify(body) : body});
  if (!response.ok) throw new Error(await response.text());
  return response.json();
 };
 for (const form of document.querySelectorAll("[data-passkey-register], [data-passkey-login]")) {
  const button = form.querySelector("button");
  button.disabled = !supported;
  if (!supported && status) status.textContent = "Passkeys sind in diesem Browser oder auf dieser Verbindung nicht verfügbar.";
  form.addEventListener("submit", async event => {
   event.preventDefault();
   if (!supported) return;
   button.disabled = true;
   if (status) status.textContent = "Bestätige den Passkey auf deinem Gerät …";
   try {
    const csrf = form.elements.csrf_token.value;
    const register = form.hasAttribute("data-passkey-register");
    const challenge = await request(form.action, csrf, register ? new URLSearchParams(new FormData(form)) : null, false);
    const credential = register ? await navigator.credentials.create(options(challenge, true)) : await navigator.credentials.get(options(challenge, false));
    if (!credential) throw new Error("Passkey-Vorgang abgebrochen.");
    const result = await request(form.dataset.finish, csrf, pack(credential), true);
    location.assign(result.redirect);
   } catch (error) {
    if (status) status.textContent = error.name === "NotAllowedError" ? "Passkey-Vorgang abgebrochen oder nicht bestätigt. Du kannst es erneut versuchen." : error.message;
    button.disabled = false;
   }
  });
 }
})();

;(() => {
 const field = document.getElementById("email-confirm-token");
 if (!field) return;
 const token = new URLSearchParams(window.location.hash.slice(1)).get("token");
 window.history.replaceState(null, "", window.location.pathname + window.location.search);
 if (!token || !/^[A-Za-z0-9_-]{32,128}$/.test(token)) return;
 field.value = token;
 document.getElementById("email-confirm-submit").disabled = false;
 document.getElementById("email-confirm-status").textContent = document.documentElement.lang === "de" ? "Du kannst deine neue E-Mail-Adresse jetzt bestätigen." : "You can now confirm your new email address.";
})();

;(() => {
 const transport = document.getElementById("mail-transport");
 if (!transport) return;
 const panels = document.querySelectorAll("[data-mail-transport]");
 const update = () => {
  for (const panel of panels) {
   panel.hidden = panel.dataset.mailTransport !== transport.value;
  }
 };
 transport.addEventListener("change", update);
 update();
})();

;(() => {
 const field=document.getElementById("invitation-token");
 if (!field) return;
 const params=new URLSearchParams(location.hash.slice(1));
 const token=params.get("token");
 history.replaceState(null,"",location.pathname+location.search);
 if (!token || !/^[A-Za-z0-9_-]{32,128}$/.test(token)) return;
 field.value=token;
 const local=document.getElementById("invitation-local-fields");
 if (local) local.hidden=params.get("mode")==="oidc";
 document.getElementById("invitation-submit").disabled=false;
 document.getElementById("invitation-status").textContent="Einladungslink geladen. Bitte die Einladung ausdrücklich annehmen.";
})();
