import { invoke, isTauri } from "@tauri-apps/api/core";

const $ = (s) => document.querySelector(s);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const short = (id) => (id || "").slice(0, 7);
const STATE_CN = { idle: "已同步", scanning: "扫描中", syncing: "同步中", "sync-preparing": "准备同步", "sync-waiting": "等待同步", error: "出错", paused: "已暂停", "scan-waiting": "等待扫描", cleaning: "清理中" };
const ENGINE_CN = { running: "同步引擎运行中", starting: "同步引擎启动中", crashed: "同步引擎异常，正在重启", stopped: "同步引擎已停止" };

let core = null; // { port, token }

async function getCore() {
  if (isTauri()) return invoke("core_info");
  // 浏览器调试：http://localhost:5173/?port=xxx&token=yyy（端口/token 见 daemon.json）
  const q = new URLSearchParams(location.search);
  return { port: Number(q.get("port")), token: q.get("token") };
}

async function api(method, path, body) {
  const r = await fetch(`http://127.0.0.1:${core.port}${path}`, {
    method,
    headers: { Authorization: `Bearer ${core.token}`, "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || `HTTP ${r.status}`);
  return data;
}

function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.classList.remove("hidden");
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => t.classList.add("hidden"), 3000);
}

async function act(fn, okMsg) {
  try { await fn(); if (okMsg) toast(okMsg); await refreshAll(); }
  catch (e) { toast("操作失败：" + e.message); }
}

// ---------- 通知 ----------
let notifyApi = null;
async function notify(body) {
  if (!$("#notify").checked) return;
  try {
    if (isTauri()) {
      notifyApi ??= await import("@tauri-apps/plugin-notification");
      let ok = await notifyApi.isPermissionGranted();
      if (!ok) ok = (await notifyApi.requestPermission()) === "granted";
      if (ok) notifyApi.sendNotification({ title: "团队同步", body });
    } else if ("Notification" in window && Notification.permission === "granted") {
      new Notification("团队同步", { body });
    }
  } catch (e) { console.warn(e); }
}

// ---------- 渲染 ----------
let myID = "";
async function refreshStatus() {
  const s = await api("GET", "/api/status");
  myID = s.myID || "";
  $("#my-id").textContent = myID || "-";
  $("#ov-sync").innerHTML = s.error ? "引擎未就绪" : s.syncing ? '<span class="dot busy"></span>同步中' : '<span class="dot on"></span>已同步';
  $("#ov-conn").textContent = s.connected ?? "-";
  $("#ov-folders").textContent = s.folders ?? "-";
  $("#engine-state").textContent = ENGINE_CN[s.engine] || s.engine;
  $("#st-version").textContent = s.syncthingVersion || "-";
  const tray = s.error ? "引擎未就绪" : s.syncing ? "同步中" : `已同步 · ${s.connected} 人在线`;
  if (isTauri()) invoke("set_tray_status", { text: tray }).catch(() => {});
}

async function refreshFolders() {
  const list = await api("GET", "/api/folders");
  $("#folder-list").innerHTML = list.length ? list.map((f) => {
    const st = f.paused ? "paused" : f.status?.state;
    const need = f.status?.needFiles ? `（还差 ${f.status.needFiles} 个）` : "";
    return `<tr><td>${esc(f.label || f.id)}</td><td>${esc(f.path)}</td>
      <td><span class="dot ${st === "idle" ? "on" : "busy"}"></span>${esc(STATE_CN[st] || st || "-")}${need}${f.conflicts ? ` · <b style="color:#dc2626">${f.conflicts} 个冲突</b>` : ""}</td>
      <td>${f.status?.localFiles ?? "-"}</td><td>${f.devices.length - 1}</td>
      <td><button data-scan="${esc(f.id)}">立即扫描</button>
      <button data-pause="${esc(f.id)}" data-paused="${f.paused}">${f.paused ? "恢复" : "暂停"}</button></td></tr>`;
  }).join("") : '<tr><td colspan="6" class="empty">还没有团队文件夹，先新建一个吧</td></tr>';

  const pend = await api("GET", "/api/pending/folders");
  $("#pending-folders").innerHTML = Object.entries(pend || {}).map(([id, p]) => {
    const [dev, info] = Object.entries(p.offeredBy)[0];
    return `<div class="pending">成员 ${esc(short(dev))} 向你共享了文件夹「${esc(info.label || id)}」
      <input id="pf-${esc(id)}" placeholder="保存到本地路径" size="40" />
      <button class="primary" data-accept-folder="${esc(id)}" data-label="${esc(info.label || id)}" data-dev="${esc(dev)}">接受</button></div>`;
  }).join("");
}

async function refreshDevices() {
  const [devs, near] = await Promise.all([api("GET", "/api/devices"), api("GET", "/api/nearby")]);
  const others = devs.filter((d) => !d.self);
  $("#device-list").innerHTML = others.length ? others.map((d) => `<tr><td>${esc(d.name || short(d.deviceID))}</td>
    <td><code>${esc(d.deviceID)}</code></td><td><span class="dot ${d.connected ? "on" : ""}"></span>${d.connected ? "在线" : "离线"}</td>
    <td><button class="danger" data-del-dev="${esc(d.deviceID)}">移除</button></td></tr>`).join("")
    : '<tr><td colspan="4" class="empty">还没有成员</td></tr>';
  $("#nearby-list").innerHTML = near.length ? near.map((n) => `<tr><td>${esc(n.name || "")} <code>${esc(n.deviceID)}</code>${n.pending ? ' <span class="badge">请求加入</span>' : ""}</td>
    <td>${esc((n.addresses || []).filter((a) => !a.includes("[")).join(", "))}</td>
    <td>${n.pending ? `<button class="primary" data-accept-dev="${esc(n.deviceID)}">同意</button> <button data-dismiss-dev="${esc(n.deviceID)}">忽略</button>`
      : `<button class="primary" data-add-dev="${esc(n.deviceID)}">添加为成员</button>`}</td></tr>`).join("")
    : '<tr><td colspan="3" class="empty">暂未发现附近设备（请确认同事已打开本软件并在同一局域网）</td></tr>';
}

async function refreshConflicts() {
  const cs = await api("GET", "/api/conflicts");
  const b = $("#conflict-badge");
  b.textContent = cs.length;
  b.classList.toggle("hidden", !cs.length);
  $("#conflict-list").innerHTML = cs.length ? cs.map((c) => `<tr><td>${esc(c.folderLabel)}</td><td>${esc(c.original)}</td>
    <td>${esc(c.device)}</td><td>${new Date(c.time).toLocaleString("zh-CN")}</td>
    <td><button data-resolve="${esc(c.path)}" data-folder="${esc(c.folderID)}" data-keep="original">保留原文件</button>
    <button data-resolve="${esc(c.path)}" data-folder="${esc(c.folderID)}" data-keep="conflict">保留冲突版本</button></td></tr>`).join("")
    : '<tr><td colspan="5" class="empty">没有冲突 🎉</td></tr>';
}

async function refreshAll() {
  try { await Promise.all([refreshStatus(), refreshFolders(), refreshDevices(), refreshConflicts()]); }
  catch (e) { $("#engine-state").textContent = "连接内核失败：" + e.message; }
}

function addActivity(msg, time) {
  const li = document.createElement("li");
  li.innerHTML = `<time>${new Date(time || Date.now()).toLocaleTimeString("zh-CN")}</time>${esc(msg)}`;
  $("#activity").prepend(li);
  while ($("#activity").children.length > 100) $("#activity").lastChild.remove();
}

function listenEvents() {
  const es = new EventSource(`http://127.0.0.1:${core.port}/api/events?token=${encodeURIComponent(core.token)}`);
  let pending = null;
  es.onmessage = (m) => {
    const e = JSON.parse(m.data);
    if (e.message) addActivity(e.message, e.time);
    if (e.notify && e.message) notify(e.message);
    clearTimeout(pending);
    pending = setTimeout(refreshAll, 500); // 合并刷新
  };
  es.onerror = () => { es.close(); setTimeout(listenEvents, 3000); };
}

// ---------- 交互 ----------
document.querySelectorAll("nav a").forEach((a) => a.addEventListener("click", () => {
  document.querySelectorAll("nav a, .tab").forEach((x) => x.classList.remove("active"));
  a.classList.add("active");
  $("#" + a.dataset.tab).classList.add("active");
}));

$("#copy-id").onclick = () => navigator.clipboard.writeText(myID).then(() => toast("已复制设备 ID"));

$("#pick-folder").onclick = async () => {
  if (!isTauri()) return toast("请直接输入路径");
  const { open } = await import("@tauri-apps/plugin-dialog");
  const p = await open({ directory: true, title: "选择要同步的文件夹" });
  if (p) $("#folder-path").value = p;
};

$("#folder-form").onsubmit = (ev) => {
  ev.preventDefault();
  const f = new FormData(ev.target);
  act(() => api("POST", "/api/folders", { label: f.get("label"), path: f.get("path") }), "已创建团队文件夹").then(() => ev.target.reset());
};

$("#device-form").onsubmit = (ev) => {
  ev.preventDefault();
  const f = new FormData(ev.target);
  act(() => api("POST", "/api/devices", { deviceID: f.get("deviceID"), name: f.get("name") }), "已添加成员").then(() => ev.target.reset());
};

document.body.addEventListener("click", (ev) => {
  const d = ev.target.dataset;
  if (d.scan) act(() => api("POST", `/api/folders/${d.scan}/scan`), "已开始扫描");
  else if (d.pause) act(() => api("POST", `/api/folders/${d.pause}/${d.paused === "true" ? "resume" : "pause"}`));
  else if (d.delDev && confirm("确定移除该成员？")) act(() => api("DELETE", `/api/devices/${d.delDev}`), "已移除");
  else if (d.addDev) { const name = prompt("给这个成员起个名字（如：张三）") ?? ""; act(() => api("POST", "/api/devices", { deviceID: d.addDev, name }), "已添加，等待对方同意"); }
  else if (d.acceptDev) act(() => api("POST", `/api/pending/devices/${d.acceptDev}/accept`), "已同意，团队文件夹将自动同步");
  else if (d.dismissDev) act(() => api("DELETE", `/api/pending/devices/${d.dismissDev}`));
  else if (d.acceptFolder) {
    const path = $(`#pf-${CSS.escape(d.acceptFolder)}`).value.trim();
    if (!path) return toast("请先填写保存路径");
    act(() => api("POST", "/api/pending/folders/accept", { folderID: d.acceptFolder, label: d.label, path, devices: [d.dev] }), "已接受");
  } else if (d.resolve) act(() => api("POST", "/api/conflicts/resolve", { folderID: d.folder, path: d.resolve, keep: d.keep }), "冲突已处理");
});

// 设置
$("#notify").checked = localStorage.getItem("notify") !== "0";
$("#notify").onchange = (e) => localStorage.setItem("notify", e.target.checked ? "1" : "0");
async function initAutostart() {
  const box = $("#autostart");
  if (!isTauri()) { box.disabled = true; return; }
  const au = await import("@tauri-apps/plugin-autostart");
  box.checked = await au.isEnabled();
  box.onchange = async () => { box.checked ? await au.enable() : await au.disable(); toast(box.checked ? "已开启开机自启" : "已关闭开机自启"); };
}

// ---------- 启动 ----------
(async function main() {
  core = await getCore();
  initAutostart().catch(console.warn);
  // 内核启动需要几秒，轮询直到就绪
  for (let i = 0; i < 60; i++) {
    try { await api("GET", "/api/status"); break; } catch { await new Promise((r) => setTimeout(r, 1000)); }
  }
  await refreshAll();
  listenEvents();
  setInterval(refreshAll, 10000);
})();
